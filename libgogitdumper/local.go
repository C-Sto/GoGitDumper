package libgogitdumper

import (
	"errors"
	"fmt"
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
		//check we aren't footgunning
		//thx @justinsteven
		if d.LocalFilePath != "" && inTrustedRoot(d.LocalFilePath, localpath) != nil {
			panic(fmt.Sprintf("tried to write outisde of output dir, is someone trying to prank you? (attempted path is %s)", d.LocalFilePath))
		}

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

func inTrustedRoot(path string, trustedRoot string) error {
	for path != "/" {
		path = filepath.Dir(path)
		if path == trustedRoot {
			return nil
		}
	}
	return errors.New("path is outside of trusted root")
}
