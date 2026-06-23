package hook_svc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
)

const (
	defaultEmailFetchLimit   = 20
	maxEmailFetchLimit       = 100
	defaultEmailPollInterval = 5 * time.Minute
	minEmailPollInterval     = 30 * time.Second
	emailNetworkTimeout      = 30 * time.Second
	maxEmailBodyBytes        = 64 * 1024
	maxEmailTextChars        = 12 * 1024
)

// MailFetcher fetches new messages from a mailbox. The service owns
// persistence, de-duplication, routing and source status updates.
type MailFetcher interface {
	FetchUnread(ctx context.Context, cfg SourceConfig, limit int) (*MailFetchResult, error)
}

type MailFetchResult struct {
	Messages    []EmailMessage
	UIDValidity uint32
	CursorReset bool
}

type EmailMessage struct {
	UID                 uint32
	MessageID           string
	Subject             string
	From                string
	To                  []string
	Cc                  []string
	Date                time.Time
	Text                string
	BodyTruncated       bool
	AttachmentsDeferred bool
}

type imapMailFetcher struct{}

type emailBodyFetch struct {
	Part      []int
	MediaType string
	Encoding  string
	Size      uint32
}

func StartEmailPoller(ctx context.Context) context.CancelFunc {
	return Hook().StartEmailPoller(ctx)
}

func (s *hookSvc) StartEmailPoller(parent context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)

	s.pollerMu.Lock()
	if s.pollerCancel != nil {
		s.pollerCancel()
	}
	s.pollerCancel = cancel
	s.pollerMu.Unlock()

	go func() {
		s.pollDueEmailSources(ctx)

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pollDueEmailSources(ctx)
			}
		}
	}()

	return func() {
		cancel()
		s.pollerMu.Lock()
		s.pollerCancel = nil
		s.pollerMu.Unlock()
	}
}

func (s *hookSvc) SyncEmailSource(ctx context.Context, req *SyncEmailSourceRequest) (*SyncEmailSourceResponse, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	limit := clampEmailFetchLimit(req.Limit)

	s.emailSyncMu.Lock()
	defer s.emailSyncMu.Unlock()

	source, err := s.requireSource(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if source.Kind != string(hook_entity.SourceKindEmail) {
		return nil, i18n.NewError(ctx, code.HookInvalidSourceType)
	}
	if !source.IsEnabled() {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	cfg := parseSourceConfig(source.ConfigJSON)
	return s.syncEmailSource(ctx, source, cfg, limit)
}

func (s *hookSvc) syncEmailSource(ctx context.Context, source *hook_entity.HookSource, cfg SourceConfig, limit int) (*SyncEmailSourceResponse, error) {
	if err := validateEmailConfig(ctx, cfg); err != nil {
		return nil, err
	}

	fetchResult, err := s.emailFetcher().FetchUnread(ctx, cfg, limit)
	if err != nil {
		if updateErr := s.markEmailSourceError(ctx, source); updateErr != nil {
			return nil, updateErr
		}
		return nil, err
	}
	if fetchResult == nil {
		fetchResult = &MailFetchResult{}
	}
	if fetchResult.CursorReset {
		cfg.LastUID = 0
	}
	if fetchResult.UIDValidity > 0 {
		cfg.UIDValidity = fetchResult.UIDValidity
	}
	messages := fetchResult.Messages

	_, agentMap, err := s.agentOptions(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := hook_repo.HookRule().ListBySource(ctx, source.ID)
	if err != nil {
		return nil, err
	}

	now := s.nowUnix()
	events := make([]*HookEventItem, 0, len(messages))
	sourceMap := map[int64]string{source.ID: source.Name}
	created := 0
	skipped := 0
	maxUID := cfg.LastUID

	for _, message := range messages {
		if message.UID > maxUID {
			maxUID = message.UID
		}
		sourceRef := emailSourceRef(message)
		if sourceRef == "" {
			skipped++
			continue
		}
		existing, err := hook_repo.HookEvent().FindBySourceRef(ctx, source.ID, sourceRef)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			skipped++
			continue
		}

		event, err := s.createEmailEvent(ctx, source, rules, agentMap, message, sourceRef, now)
		if err != nil {
			return nil, err
		}
		created++
		events = append(events, eventToItem(event, sourceMap, agentMap))
	}

	cfg.LastUID = maxUID
	source.ConnectionStatus = string(hook_entity.ConnectionConnected)
	source.LastSyncTime = now
	source.TotalCount += int64(created)
	source.ConfigJSON = serializeConfig(cfg)
	source.Updatetime = now
	if err := source.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookSource().Update(ctx, source); err != nil {
		return nil, err
	}

	return &SyncEmailSourceResponse{
		Item:    sourceToItem(source),
		Events:  events,
		Created: created,
		Skipped: skipped,
	}, nil
}

func (s *hookSvc) pollDueEmailSources(ctx context.Context) {
	if hook_repo.HookSource() == nil {
		return
	}
	sources, err := hook_repo.HookSource().List(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn("list hook email sources", zap.Error(err))
		return
	}

	now := s.nowUnix()
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		if source == nil || source.Kind != string(hook_entity.SourceKindEmail) || !source.IsEnabled() {
			continue
		}
		cfg := parseSourceConfig(source.ConfigJSON)
		if source.LastSyncTime > 0 && time.Duration(now-source.LastSyncTime)*time.Second < emailPollingInterval(cfg) {
			continue
		}
		if _, err := s.SyncEmailSource(ctx, &SyncEmailSourceRequest{ID: source.ID, Limit: defaultEmailFetchLimit}); err != nil {
			logger.Ctx(ctx).Warn("sync hook email source", zap.Int64("source_id", source.ID), zap.Error(err))
		}
	}
}

func (s *hookSvc) createEmailEvent(
	ctx context.Context,
	source *hook_entity.HookSource,
	rules []*hook_entity.HookRule,
	agentMap map[int64]*AgentOption,
	message EmailMessage,
	sourceRef string,
	now int64,
) (*hook_entity.HookEvent, error) {
	title := strings.TrimSpace(message.Subject)
	if title == "" {
		title = "(无主题)"
	}
	receivedAt := now
	if !message.Date.IsZero() {
		receivedAt = message.Date.Unix()
	}
	bodyText := limitString(message.Text, maxEmailTextChars)
	payload := map[string]any{
		"type":                  "email.received",
		"sourceId":              source.ID,
		"sourceName":            source.Name,
		"kind":                  source.Kind,
		"uid":                   message.UID,
		"messageId":             message.MessageID,
		"subject":               title,
		"title":                 title,
		"sender":                message.From,
		"from":                  message.From,
		"to":                    message.To,
		"cc":                    message.Cc,
		"date":                  message.Date.Format(time.RFC3339),
		"snippet":               limitString(collapseWhitespace(bodyText), 500),
		"bodyText":              bodyText,
		"bodyTruncated":         message.BodyTruncated,
		"attachmentsDeferred":   message.AttachmentsDeferred,
		"attachmentsDownloaded": false,
	}
	matches, dispatches := evaluateRules(source, rules, payload, agentMap)
	eventStatus := string(hook_entity.EventUnmatched)
	if len(dispatches) > 0 {
		eventStatus = string(hook_entity.EventDispatched)
	}
	payloadRaw, _ := json.MarshalIndent(payload, "", "  ")
	matchesRaw, _ := json.Marshal(matches)
	dispatchRaw, _ := json.Marshal(dispatches)

	event := &hook_entity.HookEvent{
		SourceID:         source.ID,
		Title:            title,
		SourceRef:        sourceRef,
		Sender:           message.From,
		EventType:        "email.received",
		EventStatus:      eventStatus,
		PayloadJSON:      string(payloadRaw),
		MatchedRulesJSON: string(matchesRaw),
		DispatchesJSON:   string(dispatchRaw),
		ReceivedAt:       receivedAt,
		Status:           consts.ACTIVE,
		Createtime:       now,
		Updatetime:       now,
	}
	if err := event.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.HookEvent().Create(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *hookSvc) markEmailSourceError(ctx context.Context, source *hook_entity.HookSource) error {
	source.ConnectionStatus = string(hook_entity.ConnectionError)
	source.Updatetime = s.nowUnix()
	if err := source.Check(ctx); err != nil {
		return err
	}
	return hook_repo.HookSource().Update(ctx, source)
}

func (s *hookSvc) emailFetcher() MailFetcher {
	if s.mailFetcher != nil {
		return s.mailFetcher
	}
	return imapMailFetcher{}
}

func (s *hookSvc) nowUnix() int64 {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now().Unix()
}

func (f imapMailFetcher) FetchUnread(ctx context.Context, cfg SourceConfig, limit int) (*MailFetchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg = normalizeSourceConfig(cfg)
	client, err := dialIMAP(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = client.Close()
	}()

	if err := client.Login(strings.TrimSpace(cfg.EmailAddress), cfg.AppPassword).Wait(); err != nil {
		return nil, fmt.Errorf("imap login: %w", err)
	}
	defer func() {
		_ = client.Logout().Wait()
	}()

	mailbox := strings.TrimSpace(cfg.IMAPMailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	selectData, err := client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("select mailbox %q: %w", mailbox, err)
	}
	uidValidity := selectData.UIDValidity
	sinceUID := cfg.LastUID
	cursorReset := false
	if cfg.UIDValidity > 0 && uidValidity > 0 && cfg.UIDValidity != uidValidity {
		sinceUID = 0
		cursorReset = true
	}

	criteria := &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}
	if sinceUID > 0 {
		var uidSet imap.UIDSet
		uidSet.AddRange(imap.UID(sinceUID+1), 0)
		criteria.UID = []imap.UIDSet{uidSet}
	}
	search, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search unread mail: %w", err)
	}
	uids := search.AllUIDs()
	if len(uids) == 0 {
		return &MailFetchResult{
			Messages:    []EmailMessage{},
			UIDValidity: uidValidity,
			CursorReset: cursorReset,
		}, nil
	}
	uids = selectEmailUIDBatch(uids, limit)

	uidSet := imap.UIDSetNum(uids...)
	fetched, err := client.Fetch(uidSet, &imap.FetchOptions{
		UID:           true,
		Envelope:      true,
		InternalDate:  true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch unread mail metadata: %w", err)
	}

	messages := make([]EmailMessage, 0, len(fetched))
	for _, row := range fetched {
		if row == nil {
			continue
		}
		bodyFetch := preferredEmailBodyFetch(row.BodyStructure)
		var (
			raw           []byte
			bodyTruncated bool
		)
		if bodyFetch != nil || row.BodyStructure == nil {
			raw, bodyTruncated, err = fetchEmailBodyPreview(ctx, client, row.UID, bodyFetch)
			if err != nil {
				return nil, err
			}
		}
		message := emailMessageFromFetch(row, raw, bodyFetch, bodyTruncated, bodyStructureHasAttachment(row.BodyStructure))
		messages = append(messages, message)
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].UID < messages[j].UID })
	return &MailFetchResult{
		Messages:    messages,
		UIDValidity: uidValidity,
		CursorReset: cursorReset,
	}, nil
}

func selectEmailUIDBatch(uids []imap.UID, limit int) []imap.UID {
	if len(uids) == 0 {
		return []imap.UID{}
	}
	out := append([]imap.UID(nil), uids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	limit = clampEmailFetchLimit(limit)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func fetchEmailBodyPreview(ctx context.Context, client *imapclient.Client, uid imap.UID, bodyFetch *emailBodyFetch) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if uid == 0 {
		return nil, false, nil
	}
	bodySection := &imap.FetchItemBodySection{
		Peek:    true,
		Partial: &imap.SectionPartial{Offset: 0, Size: maxEmailBodyBytes},
	}
	if bodyFetch != nil {
		bodySection.Part = bodyFetch.Part
	}
	fetched, err := client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}).Collect()
	if err != nil {
		return nil, false, fmt.Errorf("fetch unread mail body preview: %w", err)
	}
	if len(fetched) == 0 || fetched[0] == nil {
		return nil, false, nil
	}
	raw := fetched[0].FindBodySection(bodySection)
	truncated := len(raw) >= maxEmailBodyBytes
	if bodyFetch != nil && bodyFetch.Size > maxEmailBodyBytes {
		truncated = true
	}
	return raw, truncated, nil
}

func dialIMAP(ctx context.Context, cfg SourceConfig) (*imapclient.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host := strings.TrimSpace(cfg.IMAPServer)
	port := cfg.IMAPPort
	useTLS := sourceConfigUseTLS(cfg)
	if port <= 0 {
		if useTLS {
			port = 993
		} else {
			port = 143
		}
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	options := &imapclient.Options{
		Dialer:      &net.Dialer{Timeout: emailNetworkTimeout},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}
	var (
		client *imapclient.Client
		err    error
	)
	if useTLS {
		options.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		client, err = imapclient.DialTLS(address, options)
	} else {
		client, err = imapclient.DialInsecure(address, options)
	}
	if err != nil {
		return nil, fmt.Errorf("connect imap %s: %w", address, err)
	}
	return client, nil
}

func emailMessageFromFetch(row *imapclient.FetchMessageBuffer, raw []byte, bodyFetch *emailBodyFetch, bodyTruncated bool, attachmentsDeferred bool) EmailMessage {
	message := EmailMessage{
		UID:                 uint32(row.UID),
		Date:                row.InternalDate,
		BodyTruncated:       bodyTruncated,
		AttachmentsDeferred: attachmentsDeferred,
	}
	if row.Envelope != nil {
		message.MessageID = strings.TrimSpace(row.Envelope.MessageID)
		message.Subject = strings.TrimSpace(row.Envelope.Subject)
		message.From = strings.Join(formatIMAPAddresses(row.Envelope.From), ", ")
		message.To = formatIMAPAddresses(row.Envelope.To)
		message.Cc = formatIMAPAddresses(row.Envelope.Cc)
		if !row.Envelope.Date.IsZero() {
			message.Date = row.Envelope.Date
		}
	}

	if bodyFetch != nil {
		message.Text = emailBodyPartText(raw, bodyFetch)
	} else {
		text, headerMessageID, headerSubject, headerFrom, headerTo, headerCc, headerDate := parseMailBody(raw)
		message.Text = text
		if message.MessageID == "" {
			message.MessageID = headerMessageID
		}
		if message.Subject == "" {
			message.Subject = headerSubject
		}
		if message.From == "" {
			message.From = headerFrom
		}
		if len(message.To) == 0 {
			message.To = headerTo
		}
		if len(message.Cc) == 0 {
			message.Cc = headerCc
		}
		if message.Date.IsZero() {
			message.Date = headerDate
		}
	}
	return message
}

func preferredEmailBodyFetch(bs imap.BodyStructure) *emailBodyFetch {
	if bs == nil {
		return nil
	}
	var plain *emailBodyFetch
	var html *emailBodyFetch
	bs.Walk(func(path []int, part imap.BodyStructure) (walkChildren bool) {
		if isAttachmentPart(part) {
			return false
		}
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		mediaType := single.MediaType()
		candidate := &emailBodyFetch{
			Part:      clonePartPath(path),
			MediaType: mediaType,
			Encoding:  single.Encoding,
			Size:      single.Size,
		}
		if strings.EqualFold(mediaType, "text/plain") && plain == nil {
			plain = candidate
		}
		if strings.EqualFold(mediaType, "text/html") && html == nil {
			html = candidate
		}
		return plain == nil
	})
	if plain != nil {
		return plain
	}
	return html
}

func clonePartPath(path []int) []int {
	return append([]int(nil), path...)
}

func bodyStructureHasAttachment(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(_ []int, part imap.BodyStructure) (walkChildren bool) {
		if isAttachmentPart(part) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func isAttachmentPart(part imap.BodyStructure) bool {
	if part == nil {
		return false
	}
	if disposition := part.Disposition(); disposition != nil && strings.EqualFold(disposition.Value, "attachment") {
		return true
	}
	return bodyPartFilename(part) != ""
}

func bodyPartFilename(part imap.BodyStructure) string {
	single, ok := part.(*imap.BodyStructureSinglePart)
	if !ok {
		return ""
	}
	return strings.TrimSpace(single.Filename())
}

func emailBodyPartText(raw []byte, bodyFetch *emailBodyFetch) string {
	if bodyFetch == nil {
		return ""
	}
	decoded := decodeEmailBodyPart(raw, bodyFetch.Encoding)
	text := strings.ToValidUTF8(string(decoded), "")
	if strings.EqualFold(bodyFetch.MediaType, "text/html") {
		return stripHTML(text)
	}
	return text
}

func decodeEmailBodyPart(raw []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.TrimSpace(raw))))
		if err == nil {
			return decoded
		}
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
		if err == nil {
			return decoded
		}
	}
	return raw
}

func parseMailBody(raw []byte) (text string, messageID string, subject string, from string, to []string, cc []string, date time.Time) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", "", "", "", nil, nil, time.Time{}
	}
	defer func() {
		_ = reader.Close()
	}()

	messageID, _ = reader.Header.MessageID()
	subject, _ = reader.Header.Subject()
	from = formatMailAddresses(mustAddressList(reader.Header.AddressList("From")))
	to = splitAddressList(mustAddressList(reader.Header.AddressList("To")))
	cc = splitAddressList(mustAddressList(reader.Header.AddressList("Cc")))
	date, _ = reader.Header.Date()

	var htmlFallback string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		contentType, _, _ := inline.ContentType()
		body := readStringLimit(part.Body, maxEmailBodyBytes)
		if strings.EqualFold(contentType, "text/plain") && strings.TrimSpace(body) != "" {
			return strings.ToValidUTF8(body, ""), messageID, subject, from, to, cc, date
		}
		if strings.EqualFold(contentType, "text/html") && htmlFallback == "" {
			htmlFallback = stripHTML(body)
		}
	}
	return strings.ToValidUTF8(htmlFallback, ""), messageID, subject, from, to, cc, date
}

func validateEmailConfig(ctx context.Context, cfg SourceConfig) error {
	if strings.TrimSpace(cfg.IMAPServer) == "" ||
		strings.TrimSpace(cfg.EmailAddress) == "" ||
		strings.TrimSpace(cfg.AppPassword) == "" {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	if cfg.IMAPPort < 0 || cfg.IMAPPort > 65535 {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	return nil
}

func sourceConfigUseTLS(cfg SourceConfig) bool {
	return cfg.UseTLS == nil || *cfg.UseTLS
}

func emailPollingInterval(cfg SourceConfig) time.Duration {
	raw := strings.TrimSpace(cfg.PollingInterval)
	if raw == "" {
		return defaultEmailPollInterval
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return defaultEmailPollInterval
	}
	if interval < minEmailPollInterval {
		return minEmailPollInterval
	}
	return interval
}

func clampEmailFetchLimit(limit int) int {
	if limit <= 0 {
		return defaultEmailFetchLimit
	}
	if limit > maxEmailFetchLimit {
		return maxEmailFetchLimit
	}
	return limit
}

func emailSourceRef(message EmailMessage) string {
	if message.MessageID != "" {
		return message.MessageID
	}
	if message.UID > 0 {
		return fmt.Sprintf("imap:%d", message.UID)
	}
	return ""
}

func formatIMAPAddresses(addrs []imap.Address) []string {
	out := make([]string, 0, len(addrs))
	for i := range addrs {
		addr := addrs[i]
		if addr.IsGroupStart() || addr.IsGroupEnd() {
			continue
		}
		email := strings.TrimSpace(addr.Addr())
		if email == "" {
			continue
		}
		name := strings.TrimSpace(addr.Name)
		if name == "" {
			out = append(out, email)
			continue
		}
		out = append(out, fmt.Sprintf("%s <%s>", name, email))
	}
	return out
}

func mustAddressList(addrs []*mail.Address, _ error) []*mail.Address {
	return addrs
}

func splitAddressList(addrs []*mail.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr == nil || strings.TrimSpace(addr.Address) == "" {
			continue
		}
		if strings.TrimSpace(addr.Name) == "" {
			out = append(out, addr.Address)
		} else {
			out = append(out, fmt.Sprintf("%s <%s>", addr.Name, addr.Address))
		}
	}
	return out
}

func formatMailAddresses(addrs []*mail.Address) string {
	return strings.Join(splitAddressList(addrs), ", ")
}

func readStringLimit(r io.Reader, limit int64) string {
	raw, _ := io.ReadAll(io.LimitReader(r, limit))
	return string(raw)
}

func limitString(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func stripHTML(value string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}
	return collapseWhitespace(builder.String())
}
