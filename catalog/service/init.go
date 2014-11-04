package service

import (
	"log"
	"os"
	"strconv"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "[main] ", 0)

	v, err := strconv.Atoi(os.Getenv("DEBUG"))
	if err == nil && v == 1 {
		logger.SetFlags(log.Ltime | log.Lshortfile)
	}
}
