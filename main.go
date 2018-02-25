package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var commonrefs = []string{
	"FETCH_HEAD", "HEAD", "ORIG_HEAD",
	"config", "info/refs", "logs/HEAD", "logs/refs/heads/master",
	"logs/refs/remotes/origin/HEAD", "logs/refs/remotes/origin/master",
	"logs/refs/stash", "packed-refs", "refs/heads/master",
	"refs/remotes/origin/HEAD", "refs/remotes/origin/master", "refs/stash",
}

var commonfiles = []string{
	"COMMIT_EDITMSG", "description", "hooks/applypatch-msg.sample", "hooks/applypatch-msg.sample",
	"hooks/applypatch-msg.sample", "hooks/commit-msg.sample", "hooks/post-commit.sample",
	"hooks/post-receive.sample", "hooks/post-update.sample", "hooks/pre-applypatch.sample",
	"hooks/pre-commit.sample", "hooks/pre-push.sample", "hooks/pre-rebase.sample",
	"hooks/pre-receive.sample", "hooks/prepare-commit-msg.sample", "hooks/update.sample",
	"index", "info/exclude", "objects/info/packs",
}

var tested ThreadSafeSet
var url string
var localpath string

type writeme struct {
	localFilePath string
	filecontents  []byte
}

type config struct {
	Threads   int
	Url       string
	Localpath string
}

func main() {

	//setup
	cfg := config{}
	flag.IntVar(&cfg.Threads, "t", 10, "Number of concurrent threads")
	flag.StringVar(&cfg.Url, "u", "", "Url to dump (ensure the .git directory has a trailing '/')")
	flag.StringVar(&cfg.Localpath, "o", "."+string(os.PathSeparator), "Local folder to dump into")

	flag.Parse()

	if cfg.Url == "" { //todo: check for correct .git thing
		panic("Url required")
	}
	workers := cfg.Threads
	tested = ThreadSafeSet{}.Init()

	url = cfg.Url
	localpath = cfg.Localpath

	getqueue := make(chan string, 100)
	newfilequeue := make(chan string, 100)
	writefileChan := make(chan writeme, 100)

	go localWriter(writefileChan)
	go adderWorker(getqueue, newfilequeue)

	for x := 0; x < workers; x++ {
		go GetWorker(getqueue, newfilequeue, writefileChan)
	}

	//get HEAD. If this fails, we are probably going to have a bad time

	for _, x := range commonrefs {
		newfilequeue <- url + x
	}
	for _, x := range commonfiles {
		newfilequeue <- url + x
	}

	for {
		if len(getqueue) == 0 && len(newfilequeue) == 0 {
			break
		}
		time.Sleep(time.Second * 2)
	}

}

func GetWorker(c chan string, c2 chan string, localFileWriteChan chan writeme) {
	re := regexp.MustCompile("[0-9a-fA-F]{40}")
	for {

		path := <-c
		resp, err := getThing(path)
		if err != nil {
			fmt.Println(err, path)
			continue //todo: handle err better
		}
		fmt.Println("Downloaded: ", path)
		//write to local path
		d := writeme{}
		d.localFilePath = localpath + string(os.PathSeparator) + path[len(url):]
		d.filecontents = resp
		localFileWriteChan <- d

		//check if we can zlib decompress it
		zl := bytes.NewReader(resp)
		r, err := zlib.NewReader(zl)
		if err == nil {
			buf := new(bytes.Buffer)
			buf.ReadFrom(r)
			resp = buf.Bytes()
			r.Close()
		}

		//check for any sha1 objects in the ref
		match := re.FindAll(resp, -1)
		for _, x := range match {
			//add sha1's to line
			c2 <- url + "objects/" + string(x[0:2]) + "/" + string(x[2:])
		}
		//check for any refs in object (by grepping it I guess?)

	}
}

func getThing(path string) ([]byte, error) {
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("File not found")
	}
	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}

func adderWorker(getChan chan string, potentialChan chan string) {
	for {
		x := <-potentialChan
		if !tested.HasValue(x) {
			tested.Add(x)
			getChan <- x
		}

	}
}

func localWriter(writeChan chan writeme) {
	//check if our local dir exists, make if not
	if _, err := os.Stat(localpath); os.IsNotExist(err) {
		os.MkdirAll(localpath, os.ModePerm)
	}

	for {
		d := <-writeChan
		//check if we need to make dirs or whatever
		//last object after exploding on file sep is the file, so everything before that I guess
		exploded := strings.Split(d.localFilePath, "/")

		if len(exploded) > 2 {
			dirpath := filepath.Join(exploded[:len(exploded)-2]...)
			if _, err := os.Stat(dirpath); !os.IsNotExist(err) {
				os.MkdirAll(dirpath, os.ModePerm)
			}
		}

		ioutil.WriteFile(d.localFilePath, d.filecontents, 0644)
	}
}

type ThreadSafeSet struct {
	mutex *sync.RWMutex
	vals  map[string]bool
}

func (t ThreadSafeSet) Init() ThreadSafeSet {
	t = ThreadSafeSet{}
	t.mutex = &sync.RWMutex{}
	t.vals = make(map[string]bool)
	return t
}

func (t ThreadSafeSet) HasValue(s string) bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if _, ok := t.vals[s]; ok {
		return true
	}
	return false
}

func (t *ThreadSafeSet) Add(s string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.vals[s] = true

}
