package piagent

type RunOption func(*runSpec)

type runSpec struct {
	permissionMode PermissionMode
	images         []Image
}

func RunPermissionMode(mode PermissionMode) RunOption {
	return func(s *runSpec) { s.permissionMode = mode }
}

// WithImages 把多模态图片附带到本轮 prompt。Pi RPC 协议在 prompt 帧里用
// images: []ImageContent{type:"image", data:<base64>, mimeType} 透传图片，
// 不走 @file 引用，因此调用方只要给原始字节 + MIME 类型即可。
func WithImages(images []Image) RunOption {
	return func(s *runSpec) { s.images = images }
}
