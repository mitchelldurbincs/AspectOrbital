package logger

import (
	"log"
	"os"
)

func New(component string) *log.Logger {
	prefix := component
	if prefix != "" {
		prefix += " "
	}

	return log.New(os.Stdout, prefix, log.Ldate|log.Ltime|log.LUTC|log.Lmsgprefix)
}
