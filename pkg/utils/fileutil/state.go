package fileutil

import (
	"sync"
	"time"
)

type fileRecord struct {
	path      string
	readTime  time.Time
	writeTime time.Time
}

var (
	fileRecords     = make(map[string]fileRecord)
	fileRecordMutex sync.RWMutex
)

func RecordFileRead(path string) {
	fileRecordMutex.Lock()
	defer fileRecordMutex.Unlock()

	record, exists := fileRecords[path]
	if !exists {
		record = fileRecord{path: path}
	}
	record.readTime = time.Now()
	fileRecords[path] = record
}

func GetLastReadTime(path string) time.Time {
	fileRecordMutex.RLock()
	defer fileRecordMutex.RUnlock()

	record, exists := fileRecords[path]
	if !exists {
		return time.Time{}
	}
	return record.readTime
}

func RecordFileWrite(path string) {
	fileRecordMutex.Lock()
	defer fileRecordMutex.Unlock()

	record, exists := fileRecords[path]
	if !exists {
		record = fileRecord{path: path}
	}
	record.writeTime = time.Now()
	fileRecords[path] = record
}
