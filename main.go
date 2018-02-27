package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
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
	//_ "net/http/pprof"
)

var version = "0.2.1"

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
	Threads     int
	Url         string
	Localpath   string
	IndexBypass bool
}

func printBanner() {
	fmt.Println(strings.Repeat("=", 20))
	fmt.Println("GoGitDumper V" + version)
	fmt.Println("Poorly hacked together by C_Sto")
	fmt.Println(strings.Repeat("=", 20))
}

func main() {
	/*
		//profiling code - handy when dealing with concurrency and deadlocks ._.
		go func() {
			http.ListenAndServe("localhost:6061", http.DefaultServeMux)
		}()
	*/

	printBanner()
	//setup
	cfg := config{}
	flag.IntVar(&cfg.Threads, "t", 10, "Number of concurrent threads")
	flag.StringVar(&cfg.Url, "u", "", "Url to dump (ensure the .git directory has a trailing '/')")
	flag.StringVar(&cfg.Localpath, "o", "."+string(os.PathSeparator), "Local folder to dump into")
	flag.BoolVar(&cfg.IndexBypass, "i", false, "Bypass parsing the index file, but still download it")

	flag.Parse()

	if cfg.Url == "" { //todo: check for correct .git thing
		panic("Url required")
	}

	workers := cfg.Threads
	tested = ThreadSafeSet{}.Init()

	url = cfg.Url
	localpath = cfg.Localpath

	//setting the chan size to bigger than the number of workers to avoid deadlocks on high worker counts
	getqueue := make(chan string, workers*2)
	newfilequeue := make(chan string, workers*2)
	writefileChan := make(chan writeme, workers*2)

	go localWriter(writefileChan) //writes out the downloaded files

	//takes any new objects identified, and checks to see if already downloaded. will add new files to the queue if unique.
	go adderWorker(getqueue, newfilequeue)

	//downloader bois
	for x := 0; x < workers; x++ {
		go GetWorker(getqueue, newfilequeue, writefileChan)
	}

	//get the index file, parse it for files and whatnot
	fmt.Println(cfg.IndexBypass)
	if cfg.IndexBypass {
		newfilequeue <- url + "index"
	} else {
		err := getIndex(newfilequeue, writefileChan)
		if err != nil {
			panic(err)
		}
	}

	//get the packs (if any exist) and parse them out too
	getPacks(newfilequeue, writefileChan)

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

	packfile, err := getThing(url + "objects/info/packs")
	if err != nil {
		//handle error?
	}
	fmt.Println("Downloaded: ", url+"objects/info/packs")

	d := writeme{}
	d.localFilePath = localpath + string(os.PathSeparator) + "objects" + string(os.PathSeparator) + "info" + string(os.PathSeparator) + "packs"
	d.filecontents = packfile
	writefileChan <- d

	if len(packfile) > 0 {
		//this is not how packfiles work. Worst case is we accidentally download some packfiles,
		//but as the sha1 is based on the last 20 bytes (or something like that), not sure how to do this blindly
		sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
		match := sha1re.FindAll(packfile, -1) //doing dumb regex look for sha1's in packfiles, I don't think this is how it works tbh
		for _, x := range match {

			newfilequeue <- url + "objects/pack/pack-" + string(x) + ".idx"
			newfilequeue <- url + "objects/pack/pack-" + string(x) + ".pack"
		}

	}
}

func getIndex(newfileChan chan string, localfileChan chan writeme) error {

	indexfile, err := getThing(url + "index")
	if err != nil {
		return err
	}

	fmt.Println("Downloaded: ", url+"index")

	d := writeme{}
	d.localFilePath = localpath + string(os.PathSeparator) + "index"
	d.filecontents = indexfile
	localfileChan <- d

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

func GetWorker(c chan string, c2 chan string, localFileWriteChan chan writeme) {
	sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
	refre := regexp.MustCompile(`(refs(/[a-zA-Z0-9\-\.\_\*]+)+)`)
	localbuffer := []string{}
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

			select { //this is a gross hack to stop deadlocking on the relatively small channel. Sorry not sorry
			case c2 <- url + "objects/" + string(x[0:2]) + "/" + string(x[2:]):
				continue
			default:
				localbuffer = append(localbuffer, url+"objects/"+string(x[0:2])+"/"+string(x[2:]))
			}

		}

		//check for ref paths in the thing
		match = refre.FindAll(resp, -1)
		for _, x := range match {
			if string(x[len(x)-1]) == "*" {
				continue
			}

			//very gross hacks not happy
			select {
			case c2 <- url + string(x):
			default:
				localbuffer = append(localbuffer, url+string(x))
			}

			select {
			case c2 <- url + "logs/" + string(x):
			default:
				localbuffer = append(localbuffer, url+"logs/"+string(x))
			}

		}

		//attempt to write the rest of the things, then give up if you can't
		for _, x := range localbuffer {
			select {
			case c2 <- x:
			default:
				continue
			}
		}
		fmt.Println(localbuffer)

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
		return nil, errors.New(fmt.Sprintf("Error code: %d\n", resp.StatusCode))
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

func parseIndexFile(b []byte) (indexFile, error) {
	// thanks to this guy https://github.com/sbp/gin/blob/master/gin
	indx := indexFile{}
	readcount := uint16(0) //easier this way
	//4 byte signature "DIRC"
	readcount += 4
	if string(b[:readcount]) != "DIRC" {
		return indexFile{}, errors.New("Bad index file")
	}
	indx.Signature = string(b[:readcount])

	//4 byte version number (32bit int)
	indx.Version = binary.BigEndian.Uint32(b[readcount : readcount+4])
	if indx.Version != 2 && indx.Version != 3 {
		return indexFile{}, errors.New("Bad index file")
	}
	readcount += 4

	//4 byte count of index entries
	indx.EntryCount = binary.BigEndian.Uint32(b[readcount : readcount+4])
	readcount += 4

	//for each entry
	for x := uint32(1); x <= indx.EntryCount; x++ {
		entryLen := uint16(0)
		entry := indexEntry{}
		entry.Number = x

		entry.Ctime_seconds = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Ctime_nanoseconds = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Mtime_seconds = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Mtime_nanoseconds = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4

		entry.Dev = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Ino = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4

		entry.Mode = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Uid = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Gid = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4
		entry.Size = binary.BigEndian.Uint32(b[readcount : readcount+4])
		readcount += 4
		entryLen += 4

		entry.Sha1 = hex.EncodeToString(b[readcount : readcount+20])
		readcount += 20
		entryLen += 20

		entry.Flags = binary.BigEndian.Uint16(b[readcount : readcount+2])
		//entry.copy(entry.Flags[:], b[readcount:readcount+2])
		readcount += 2
		entryLen += 2

		if entry.Flags&(128<<8) > 0 {
			entry.Flag_assumevalid = true
		}
		if entry.Flags&(64<<8) > 0 {
			entry.Flag_extended = true
		}
		if entry.Flags&(32<<8) > 0 {
			entry.Flag_stage1 = true
		}
		if entry.Flags&(16<<8) > 0 {
			entry.Flag_stage2 = true
		}

		if entry.Flag_extended && indx.Version == 3 {
			fmt.Println("hax")
			entry.ExtraFlags = binary.BigEndian.Uint16(b[readcount : readcount+2])
			readcount += 2
			entryLen += 2
			//idc about any of this I don't think
		}

		entry.Flag_nameLen = entry.Flags & 0xfff //this is not what should happen - need to check if it's above fff here?

		if entry.Flag_nameLen < 0xfff { //we literally just made it below 0xfff, so this will always happen... I think
			entry.Name = string(b[readcount : readcount+entry.Flag_nameLen])
			readcount += entry.Flag_nameLen
			entryLen += entry.Flag_nameLen

		}

		//there is probably a better way of doing this
		padlen := (8 - (entryLen % 8))
		if padlen == 0 {
			padlen = 8
		}

		//ensure all the supposed pad bytes are nulls

		for x := uint16(0); x < padlen; x++ {
			test := b[readcount : readcount+1]
			readcount += 1
			if test[0] != 0x00 {
				return indexFile{}, errors.New("Index entry padding error")
			}
		}

		indx.Entries = append(indx.Entries, entry)
	}

	return indx, nil
}

type indexFile struct {
	Signature  string //should be "DIRC"
	Version    uint32
	EntryCount uint32
	Entries    []indexEntry
}

type indexEntry struct {
	Number            uint32
	Ctime_seconds     uint32 //32 bit number I guess?
	Ctime_nanoseconds uint32 //as above
	Mtime_seconds     uint32 //32 bit number I guess?
	Mtime_nanoseconds uint32 //as above
	Dev               uint32 //idk lol
	Ino               uint32 // ^^
	Mode              uint32 //4 bit object type, 3 bits unused, 9 bit unix permission
	Uid               uint32
	Gid               uint32
	Size              uint32
	Sha1              string // [20]byte (converted to a string because it's easier that way)

	Flags            uint16 // 1 bit assume-valid, 1 bit extended, 2 bit stage, 12 bit name length if length <  0xFF, otherwise 0xFFF
	Flag_assumevalid bool
	Flag_extended    bool
	Flag_stage1      bool
	Flag_stage2      bool
	Flag_nameLen     uint16 //actually 12 bit

	ExtraFlags uint16 //1bit reserved, 1bit skip-worktree, 1bit intent-to-add, 13 bits unused
	Name       string //variable length name, because of course

	Ext_signature [4]byte
	Ext_size      [4]byte //32bit int

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
