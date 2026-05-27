package claudecodehook

import "io"

// EmitNoop writes the minimal valid hook output when the gateway env is
// missing. Lets the claude CLI continue without interference.
func EmitNoop(event string, out io.Writer) {
	switch event {
	case "post-tool":
		emitPostToolContext(out, "")
	default:
		_, _ = out.Write([]byte("{}\n"))
	}
}
