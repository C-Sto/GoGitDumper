package libgogitdumper

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

func LocalWriter(writeChan chan Writeme, localpath string, fileCount *uint64, byteCount *uint64, wgSaveFile *sync.WaitGroup) {
	//check if our local dir exists, make if not
	if _, err := os.Stat(localpath); os.IsNotExist(err) {
		os.MkdirAll(localpath, os.ModePerm)
	}

	for {
		d := <-writeChan
		//check if we need to make dirs or whatever
		//last object after exploding on file sep is the file, so everything before that I guess
		dirpath := filepath.Dir(d.LocalFilePath)
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			os.MkdirAll(dirpath, os.ModePerm)
		}
		ioutil.WriteFile(d.LocalFilePath, d.Filecontents, 0644)
		atomic.AddUint64(fileCount, 1)
		atomic.AddUint64(byteCount, uint64(len(d.Filecontents)))
		//signal that file is written (or at least, the above line has finished executing)
		wgSaveFile.Done()
	}
}
