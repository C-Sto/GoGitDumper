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

var version = "0.1"

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
	"info/exclude",
	//these are obtained individually to be parsed for goodies
	//"objects/info/packs",
	//"index",
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

func printBanner() {
	fmt.Println(strings.Repeat("=", 20))
	fmt.Println("GoGitDumper V" + version)
	fmt.Println("Poorly hacked together by C_Sto")
	fmt.Println(strings.Repeat("=", 20))
}

func main() {
	printBanner()
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

	//setting the chan size to slightly bigger than the number of workers to avoid deadlocks on high worker counts
	getqueue := make(chan string, workers+5)
	newfilequeue := make(chan string, workers+5)
	writefileChan := make(chan writeme, workers+5)

	//todo: check url is good
	//get index. If this fails, we are probably going to have a bad time

	go localWriter(writefileChan) //writes out the downloaded files

	//takes any new objects identified, and checks to see if already downloaded. will add new files to the queue if unique.
	go adderWorker(getqueue, newfilequeue)

	//downloader bois
	for x := 0; x < workers; x++ {
		go GetWorker(getqueue, newfilequeue, writefileChan)
	}

	//get the index file, parse it for files and whatnot
	err := getIndex(newfilequeue, writefileChan)
	if err != nil {
		panic(err)
	}

	//get the packs (if any exist) and parse them out too
	go getPacks(newfilequeue, writefileChan)

	//get all the common things that contain refs
	for _, x := range commonrefs {
		newfilequeue <- url + x
	}

	//get all the common files that may be important I guess?
	for _, x := range commonfiles {
		newfilequeue <- url + x
	}

	//todo: make this wait for closed channels and such
	for {
		if len(getqueue) == 0 && len(newfilequeue) == 0 {
			break
		}
		time.Sleep(time.Second * 2)
	}

}

func getPacks(newfilequeue chan string, writefileChan chan writeme) {
	//todo: parse packfiles for new objects and whatnot
	//get packfiles from objects/info/packs
	sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
	packfile, err := getThing(url + "objects/info/packs")
	if err != nil {
		//handle error?
	}
	if len(packfile) > 0 {
		match := sha1re.FindAll(packfile, -1)
		for _, x := range match {
			newfilequeue <- url + "/objects/pack/pack-" + string(x) + ".idx"
			newfilequeue <- url + "/objects/pack/pack-" + string(x) + ".pack"
		}
	}
}

func getIndex(newfileChan chan string, localfileChan chan writeme) error {

	indexfile, err := getThing(url + "index")
	if err != nil {
		return err
	}

	//write file, async to avoid dumb blocking
	go func() {
		d := writeme{}
		d.localFilePath = localpath + string(os.PathSeparator) + "index"
		d.filecontents = indexfile
		localfileChan <- d
	}()

	parsed, err := parseIndexFile(indexfile)
	if err != nil {
		//deal with parsing error X_X (not blocking for now)
		return nil
	}

	for _, x := range parsed.Entries {
		newfileChan <- url + "objects/" + string(x.Sha1[0:2]) + "/" + string(x.Sha1[2:])
	}

	return err

}

func parseIndexFile(b []byte) (indexFile, error) {
	return indexFile{}, nil
}

func GetWorker(c chan string, c2 chan string, localFileWriteChan chan writeme) {
	sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
	refre := regexp.MustCompile(`(refs(/[a-zA-Z0-9\-\.\_\*]+)+)`)
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

		//check for any sha1 objects in the thing
		match := sha1re.FindAll(resp, -1)
		for _, x := range match {
			//add sha1's to line
			c2 <- url + "objects/" + string(x[0:2]) + "/" + string(x[2:])
		}

		//check for ref paths in the thing
		match = refre.FindAll(resp, -1)
		for _, x := range match {
			if string(x[len(x)-1]) == "*" {
				continue
			}
			c2 <- url + string(x)
			c2 <- url + "logs/" + string(x)
		}

	}
}

func getThing(path string) ([]byte, error) {
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("404 File not found")
	} else if resp.StatusCode != 200 {
		return nil, errors.New("Error code: " + string(resp.StatusCode))
	}
	body, err := ioutil.ReadAll(resp.Body)
	if strings.Contains(string(body), "<title>Directory listing for ") {
		return nil, errors.New("Found directory indexing, consider using recursive grep to mirror")
	}
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
		dirpath := filepath.Dir(d.localFilePath)
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			os.MkdirAll(dirpath, os.ModePerm)
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

func readIndex(b []byte) (indexFile, error) {
	return indexFile{}, nil
}

type indexFile struct {
	Signature  [4]byte //should be "DIRC"
	Version    [4]byte //should be 2 or 3
	EntryCount [4]byte //32bit number
	Entries    []indexEntry
}

type indexEntry struct {
	Ctime_seconds     [4]byte //32 bit number I guess?
	Ctime_nanoseconds [4]byte //as above
	Mtime_seconds     [4]byte //32 bit number I guess?
	Mtime_nanoseconds [4]byte //as above
	Dev               [4]byte
	Ino               [4]byte
	Mode              [4]byte //4 bit object type, 3 bits unused, 9 bit unix permission
	Uid               [4]byte
	Gid               [4]byte
	Size              [4]byte
	Sha1              [20]byte
	Flags             [2]byte // 1 bit assume-valid, 1 bit extended, 2 bit stage, 12 bit name length if length <  0xFF, otherwise 0xFFF
	ExtraFlags        [2]byte //1bit reserved, 1bit skip-worktree, 1bit intent-to-add, 13 bits unused
	Name              []byte  //variable length name, because of course
	Ext_signature     [4]byte
	Ext_size          [4]byte //32bit int

}

//the actual .pack file
type packFile struct {
	//first 12 bytes are meta-info
	Header      [4]byte //should be 'PACK'
	Version     [4]byte //version - probably 0,0,0,2 or someth
	ObjectCount [4]byte //count of all objects in the file

	//last 20 bytes are a checksum
	checksum [20]byte
}

type packfileObjects struct {
}

type packIndex struct {
	//first 8 bytes is header
	Header  [4]byte //should be 255,116,79,99
	Version [4]byte //should be 0,0,0,2

}
