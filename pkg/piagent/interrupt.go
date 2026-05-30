package piagent

import "os"

func interruptSignal() os.Signal { return os.Interrupt }
