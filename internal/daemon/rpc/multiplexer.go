package rpc

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	relayEnvelopeHeaderSize = 2
	maxRelayChannelIDLength = 128
	virtualChannelBuffer    = 64
)

// relayHub is the HubLink surface used by a Multiplexer. Keeping this narrow
// lets multiplexer tests prove channel behavior without a real WebSocket.
type relayHub interface {
	Send(messageType int, payload []byte) error
	Receive() <-chan HubFrame
	AddLifecycleListener(onDial func(), onDisconnect func(error))
	Connected() bool
}

// Multiplexer carries multiple logical JSON-RPC transports over one HubLink.
// It owns channel IDs for locally opened channels and accepts a new channel
// when the relay first sends an unfamiliar channel ID.
type Multiplexer struct {
	hub relayHub

	mu       sync.RWMutex
	channels map[string]*VirtualChannel
	// retired drops stale envelopes after a local channel closes. A reconnect
	// starts a new relay lifetime, so onDisconnect clears this set.
	retired   map[string]struct{}
	accept    chan *VirtualChannel
	connected bool
	closed    bool

	stop     chan struct{}
	stopOnce sync.Once
}

// NewMultiplexer layers virtual channels over a daemon-owned HubLink. Create
// it before or after HubLink.Run; lifecycle listeners synchronize its view of
// the current physical connection.
func NewMultiplexer(link *HubLink) *Multiplexer {
	return newMultiplexer(link)
}

func newMultiplexer(hub relayHub) *Multiplexer {
	m := &Multiplexer{
		hub:       hub,
		channels:  make(map[string]*VirtualChannel),
		retired:   make(map[string]struct{}),
		accept:    make(chan *VirtualChannel, virtualChannelBuffer),
		connected: hub.Connected(),
		stop:      make(chan struct{}),
	}
	hub.AddLifecycleListener(m.onDial, m.onDisconnect)
	go m.receive()
	return m
}

// Open creates a locally initiated virtual channel. Its ID is opaque to Conn
// and appears only inside relay envelopes between the daemon and server.
func (m *Multiplexer) Open() (*VirtualChannel, error) {
	for {
		id, err := newRelayChannelID()
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrConnClosed
		}
		_, active := m.channels[id]
		_, retired := m.retired[id]
		if active || retired {
			m.mu.Unlock()
			continue
		}
		channel := newVirtualChannel(m, id)
		m.channels[id] = channel
		m.mu.Unlock()
		return channel, nil
	}
}

// Accept returns virtual channels initiated by the relay peer. The first
// valid envelope for a previously unseen channel ID creates and announces it.
func (m *Multiplexer) Accept() <-chan *VirtualChannel { return m.accept }

// Close tears down every virtual channel but deliberately leaves the physical
// HubLink running: a multiplexer must never take down LAN or relay peers.
func (m *Multiplexer) Close() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		channels := m.takeChannelsLocked()
		m.mu.Unlock()
		for _, channel := range channels {
			channel.markClosed()
		}
		close(m.stop)
	})
}

func (m *Multiplexer) receive() {
	frames := m.hub.Receive()
	for {
		select {
		case <-m.stop:
			return
		case frame, ok := <-frames:
			if !ok {
				m.onDisconnect(io.EOF)
				return
			}
			m.dispatch(frame)
		}
	}
}

func (m *Multiplexer) dispatch(frame HubFrame) {
	if frame.MessageType != websocket.BinaryMessage {
		return
	}
	channelID, payload, err := unmarshalRelayEnvelope(frame.Payload)
	if err != nil {
		return
	}

	m.mu.Lock()
	if m.closed || !m.connected {
		m.mu.Unlock()
		return
	}
	if _, retired := m.retired[channelID]; retired {
		m.mu.Unlock()
		return
	}
	channel := m.channels[channelID]
	created := channel == nil
	if created {
		channel = newVirtualChannel(m, channelID)
		m.channels[channelID] = channel
	}
	m.mu.Unlock()

	if created {
		select {
		case m.accept <- channel:
		case <-m.stop:
			_ = channel.Close()
			return
		}
	}
	channel.enqueue(payload)
}

func (m *Multiplexer) onDial() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.connected = true
	}
}

func (m *Multiplexer) onDisconnect(error) {
	m.mu.Lock()
	m.connected = false
	channels := m.takeChannelsLocked()
	m.retired = make(map[string]struct{})
	m.mu.Unlock()
	for _, channel := range channels {
		channel.markClosed()
	}
}

func (m *Multiplexer) closeChannel(channel *VirtualChannel) {
	m.mu.Lock()
	if m.channels[channel.id] == channel {
		delete(m.channels, channel.id)
		m.retired[channel.id] = struct{}{}
	}
	m.mu.Unlock()
	channel.markClosed()
}

func (m *Multiplexer) write(channel *VirtualChannel, payload []byte) error {
	m.mu.RLock()
	active := !m.closed && m.channels[channel.id] == channel
	connected := m.connected
	m.mu.RUnlock()
	if !active {
		return ErrConnClosed
	}
	if !connected {
		return ErrHubUnavailable
	}
	return m.hub.Send(websocket.BinaryMessage, marshalRelayEnvelope(channel.id, payload))
}

func (m *Multiplexer) takeChannelsLocked() []*VirtualChannel {
	channels := make([]*VirtualChannel, 0, len(m.channels))
	for _, channel := range m.channels {
		channels = append(channels, channel)
	}
	m.channels = make(map[string]*VirtualChannel)
	return channels
}

// VirtualChannel is one logical FrameConn carried by a Multiplexer.
type VirtualChannel struct {
	mux *Multiplexer
	id  string

	inbound chan []byte
	done    chan struct{}

	writeMu   sync.Mutex
	closeOnce sync.Once
}

var _ FrameConn = (*VirtualChannel)(nil)

func newVirtualChannel(mux *Multiplexer, id string) *VirtualChannel {
	return &VirtualChannel{
		mux:     mux,
		id:      id,
		inbound: make(chan []byte, virtualChannelBuffer),
		done:    make(chan struct{}),
	}
}

// ID returns the relay-local opaque channel identifier.
func (c *VirtualChannel) ID() string { return c.id }

// WriteFrame serializes an existing JSON-RPC Frame and puts those exact bytes
// after the relay envelope header. It never modifies the Frame schema.
func (c *VirtualChannel) WriteFrame(frame Frame) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return ErrConnClosed
	default:
	}
	return c.mux.write(c, payload)
}

// ReadFrame waits for the next relay payload for this channel only.
func (c *VirtualChannel) ReadFrame(frame *Frame) error {
	select {
	case payload := <-c.inbound:
		return json.Unmarshal(payload, frame)
	case <-c.done:
		return io.EOF
	}
}

// Close closes this logical channel only. It does not close the shared HubLink
// or any sibling virtual channel.
func (c *VirtualChannel) Close() error {
	c.mux.closeChannel(c)
	return nil
}

// Done closes when this channel or its shared HubLink disconnects.
func (c *VirtualChannel) Done() <-chan struct{} { return c.done }

func (c *VirtualChannel) enqueue(payload []byte) {
	select {
	case <-c.done:
		return
	case c.inbound <- payload:
	}
}

func (c *VirtualChannel) markClosed() {
	c.writeMu.Lock()
	c.closeOnce.Do(func() { close(c.done) })
	c.writeMu.Unlock()
}

// The relay envelope is binary: uint16 big-endian channel-ID byte length,
// UTF-8 channel ID bytes, then the unmodified JSON-RPC frame bytes. The relay
// WebSocket carries it as a BinaryMessage. This format exists only between
// daemon and server, never at Conn's JSON-RPC boundary.
func marshalRelayEnvelope(channelID string, frame []byte) []byte {
	channelIDBytes := []byte(channelID)
	payload := make([]byte, relayEnvelopeHeaderSize+len(channelIDBytes)+len(frame))
	binary.BigEndian.PutUint16(payload, uint16(len(channelIDBytes)))
	copy(payload[relayEnvelopeHeaderSize:], channelIDBytes)
	copy(payload[relayEnvelopeHeaderSize+len(channelIDBytes):], frame)
	return payload
}

func unmarshalRelayEnvelope(payload []byte) (string, []byte, error) {
	if len(payload) < relayEnvelopeHeaderSize {
		return "", nil, errors.New("relay envelope is missing its channel ID length")
	}
	channelIDLength := int(binary.BigEndian.Uint16(payload[:relayEnvelopeHeaderSize]))
	if channelIDLength == 0 || channelIDLength > maxRelayChannelIDLength {
		return "", nil, errors.New("relay envelope has an invalid channel ID length")
	}
	frameStart := relayEnvelopeHeaderSize + channelIDLength
	if len(payload) <= frameStart {
		return "", nil, errors.New("relay envelope is missing its JSON-RPC frame")
	}
	channelIDBytes := payload[relayEnvelopeHeaderSize:frameStart]
	if !utf8.Valid(channelIDBytes) {
		return "", nil, errors.New("relay envelope channel ID is not UTF-8")
	}
	return string(channelIDBytes), payload[frameStart:], nil
}

func newRelayChannelID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
