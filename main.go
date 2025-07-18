package main

import (
	"errors"
	"flag"
	"github.com/fsnotify/fsnotify"
	"golang.design/x/clipboard"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func ensureDir(dirName string) error {
	err := os.Mkdir(dirName, 0750)
	if err == nil {
		return nil
	}
	if !os.IsExist(err) {
		return err
	}

	// check that the existing path is a directory
	info, err := os.Stat(dirName)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path exists but is not a directory")
	}

	return nil
}

func addFileToClipboard(filePath string) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if len(fileContent) == 0 {
		return errors.New("file content is empty")
	}

	contentType := http.DetectContentType(fileContent)

	if strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/vnd.*") ||
		contentType == "application/rtf" ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "application/pdf" {
		clipboard.Write(clipboard.FmtText, fileContent)
		return nil
	}

	if strings.HasPrefix(contentType, "image/") {
		clipboard.Write(clipboard.FmtImage, fileContent)
		return nil
	}

	return errors.New("unsupported content type:" + contentType)
}

func main() {
	// only log warnings by default, don't log too much to users disk
	logLevel := slog.LevelWarn

	// allow passing log-level for debugging
	flag.Func("log-level", "set slog level (DEBUG, INFO, WARN, ERROR)", func(s string) error {
		return logLevel.UnmarshalText([]byte(s))
	})

	watchDirParam := flag.String("watch-dir",
		"",
		"Directory to watch for files",
	)
	flag.Parse()

	logger := createLogger(logLevel)

	logger.Info("Starting up fs-clip.")

	watchPath := determineWatchPath(*watchDirParam)

	err := ensureDir(watchPath)
	if err != nil {
		panic(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	err = clipboard.Init()
	if err != nil {
		panic(err)
	}

	go watchEvents(watcher, logger)

	err = watcher.Add(watchPath)
	if err != nil {
		panic(err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}

func determineWatchPath(watchDirParam string) string {
	if watchDirParam != "" {
		absoluteWatchDirParam, err := filepath.Abs(watchDirParam)
		if err != nil {
			panic(err)
		}

		return absoluteWatchDirParam
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	return homeDir + string(os.PathSeparator) + "fs-clip-watch"
}

func createLogger(level slog.Level) *slog.Logger {
	// create custpm logger
	logHandlerOptions := &slog.HandlerOptions{Level: level}
	logHandler := slog.NewTextHandler(os.Stderr, logHandlerOptions)
	logger := slog.New(logHandler)
	return logger
}

func watchEvents(watcher *fsnotify.Watcher, logger *slog.Logger) {
	// mutex to avoid concurrently writing to timers
	mu := sync.Mutex{}
	// timers to keep track of the files we are "manging" and want to write to clipboard once no more writes happen
	timers := make(map[string]*time.Timer)

	timerWait := 100 * time.Millisecond

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			fileName := event.Name

			if event.Has(fsnotify.Create) {
				handleFileCreateEvent(timerWait, fileName, logger, &mu, timers)
			}

			if event.Has(fsnotify.Write) {
				handleFileWriteEvent(timerWait, fileName, &mu, timers)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Error("File watcher error:", err)
		}
	}
}

func handleFileWriteEvent(timerWait time.Duration, fileName string, mu *sync.Mutex, timers map[string]*time.Timer) {
	// on file write we reset the timer, if now more writes happen to the file we can happily write it to
	// the clipboard
	mu.Lock()
	timer, ok := timers[fileName]
	mu.Unlock()

	// if no timer exists for the file, we treat it as not managed by fs-clip
	if !ok {
		return
	}

	timer.Reset(timerWait)
	return
}

func handleFileCreateEvent(timerWait time.Duration, fileName string, logger *slog.Logger, mu *sync.Mutex, timers map[string]*time.Timer) {
	// creating a new file in the watched folder means it will be "managed" by fs-clip
	// we create a timer to copy the file to clipboard once no more writes happen
	writeToClipboardTimer := time.AfterFunc(timerWait, func() {
		onWriteTimerEnd(fileName, logger, mu, timers)
	})

	mu.Lock()
	timers[fileName] = writeToClipboardTimer
	mu.Unlock()
}

func onWriteTimerEnd(fileName string, logger *slog.Logger, mu *sync.Mutex, timers map[string]*time.Timer) {
	err := addFileToClipboard(fileName)
	if err != nil {
		// fail silently, shouldn't interrupt the program
		logger.Error("error while adding file to clipboard:", err)
		return
	}

	err = os.Remove(fileName)
	if err != nil {
		// fail silently, shouldn't interrupt the program
		logger.Error("error while removing file:", err)
	}

	mu.Lock()
	delete(timers, fileName)
	mu.Unlock()
}
