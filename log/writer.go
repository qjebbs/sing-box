package log

import "io"

// LevelWriter is an io.Writer that is told the level of each message.
type LevelWriter interface {
	io.Writer
	WriteLevel(level Level, p []byte) (int, error)
}

// LevelSplitWriter routes warn and above to Stderr and the rest to Stdout.
type LevelSplitWriter struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (w LevelSplitWriter) Write(p []byte) (int, error) {
	return w.Stdout.Write(p)
}

func (w LevelSplitWriter) WriteLevel(level Level, p []byte) (int, error) {
	if level <= LevelWarn {
		return w.Stderr.Write(p)
	}
	return w.Stdout.Write(p)
}
