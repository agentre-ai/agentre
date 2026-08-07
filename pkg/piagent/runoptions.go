package piagent

import "strings"

type RunOption func(*runSpec)

type runSpec struct {
	permissionMode    PermissionMode
	images            []Image
	forkAnchor        string
	captureUserAnchor bool
}

func RunPermissionMode(mode PermissionMode) RunOption {
	return func(s *runSpec) { s.permissionMode = mode }
}

// RunForkAnchor forks the resumed Pi session before sending the prompt. Empty
// anchors preserve the normal prompt flow. A real fork also captures this turn's
// first native user entry for later forks.
func RunForkAnchor(anchor string) RunOption {
	return func(s *runSpec) {
		s.forkAnchor = anchor
		if strings.TrimSpace(anchor) != "" {
			s.captureUserAnchor = true
		}
	}
}

// RunCaptureUserAnchor records the first native Pi user entry created after the
// pre-prompt tree leaf without forking the current session.
func RunCaptureUserAnchor() RunOption {
	return func(s *runSpec) { s.captureUserAnchor = true }
}

// WithImages 把多模态图片附带到本轮 prompt。Pi RPC 协议在 prompt 帧里用
// images: []ImageContent{type:"image", data:<base64>, mimeType} 透传图片，
// 不走 @file 引用，因此调用方只要给原始字节 + MIME 类型即可。
func WithImages(images []Image) RunOption {
	return func(s *runSpec) { s.images = images }
}
