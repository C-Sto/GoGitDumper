package main

import (
	"bytes"
	"compress/zlib"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	//_ "net/http/pprof"
	"github.com/c-sto/gogitdumper/libgogitdumper"
)

var version = "0.4.2"

var commonrefs = []string{
	"", //check for indexing
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

var tested libgogitdumper.ThreadSafeSet
var url string
var localpath string

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
	cfg := libgogitdumper.Config{}
	var SSLIgnore bool
	flag.IntVar(&cfg.Threads, "t", 10, "Number of concurrent threads")
	flag.StringVar(&cfg.Url, "u", "", "Url to dump (ensure the .git directory has a trailing '/')")
	flag.StringVar(&cfg.Localpath, "o", "."+string(os.PathSeparator), "Local folder to dump into")
	flag.BoolVar(&cfg.IndexBypass, "i", false, "Bypass parsing the index file, but still download it")
	flag.StringVar(&cfg.IndexLocation, "l", "", "Location of a local index file to parse instead of getting it using this tool")
	flag.BoolVar(&SSLIgnore, "k", false, "Ignore SSL check")

	flag.Parse()

	if cfg.Url == "" { //todo: check for correct .git thing
		panic("Url required")
	}

	//skip ssl errors if requested to
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: SSLIgnore}

	workers := cfg.Threads
	tested = libgogitdumper.ThreadSafeSet{}.Init()

	url = cfg.Url
	localpath = cfg.Localpath

	//setting the chan size to bigger than the number of workers to avoid deadlocks on high worker counts
	getqueue := make(chan string, workers*2)
	newfilequeue := make(chan string, workers*2)
	writefileChan := make(chan libgogitdumper.Writeme, workers*2)

	go libgogitdumper.LocalWriter(writefileChan, localpath) //writes out the downloaded files

	//takes any new objects identified, and checks to see if already downloaded. will add new files to the queue if unique.
	go adderWorker(getqueue, newfilequeue)

	//downloader bois
	for x := 0; x < workers; x++ {
		go GetWorker(getqueue, newfilequeue, writefileChan)
	}

	//get the index file, parse it for files and whatnot
	if cfg.IndexBypass {
		newfilequeue <- url + "index"
	} else if cfg.IndexLocation != "" {
		indexfile, err := ioutil.ReadFile(cfg.IndexLocation)
		if err != nil {
			panic("Could not read index file: " + err.Error())
		}
		err = getIndex(indexfile, newfilequeue, writefileChan)
		if err != nil {
			panic(err)
		}
	} else {
		indexfile, err := libgogitdumper.GetThing(url + "index")
		if err != nil {
			panic(err)
		}

		err = getIndex(indexfile, newfilequeue, writefileChan)
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

func getPacks(newfilequeue chan string, writefileChan chan libgogitdumper.Writeme) {
	//todo: parse packfiles for new objects and whatnot
	//get packfiles from objects/info/packs

	packfile, err := libgogitdumper.GetThing(url + "objects/info/packs")
	if err != nil {
		//handle error?
	}
	fmt.Println("Downloaded: ", url+"objects/info/packs")

	d := libgogitdumper.Writeme{}
	d.LocalFilePath = localpath + string(os.PathSeparator) + "objects" + string(os.PathSeparator) + "info" + string(os.PathSeparator) + "packs"
	d.Filecontents = packfile
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

func getIndex(indexfile []byte, newfileChan chan string, localfileChan chan libgogitdumper.Writeme) error {

	fmt.Println("Downloaded: ", url+"index")

	d := libgogitdumper.Writeme{}
	d.LocalFilePath = localpath + string(os.PathSeparator) + "index"
	d.Filecontents = indexfile
	localfileChan <- d

	parsed, err := libgogitdumper.ParseIndexFile(indexfile)
	if err != nil {
		//deal with parsing error X_X (not blocking for now)
		return nil
	}

	for _, x := range parsed.Entries {
		newfileChan <- url + "objects/" + string(x.Sha1[0:2]) + "/" + string(x.Sha1[2:])
	}

	return err

}

func GetWorker(c chan string, c2 chan string, localFileWriteChan chan libgogitdumper.Writeme) {
	sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
	refre := regexp.MustCompile(`(refs(/[a-zA-Z0-9\-\.\_\*]+)+)`)
	for {

		path := <-c
		resp, err := libgogitdumper.GetThing(path)
		if err != nil {
			fmt.Println(err, path)
			continue //todo: handle err better
		}
		fmt.Println("Downloaded: ", path)
		//write to local path
		d := libgogitdumper.Writeme{}
		d.LocalFilePath = localpath + string(os.PathSeparator) + path[len(url):]
		d.Filecontents = resp
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

func adderWorker(getChan chan string, potentialChan chan string) {
	for {
		x := <-potentialChan
		if !tested.HasValue(x) {
			tested.Add(x)
			select {
			case getChan <- x:
				//do nothing (this should avoid spinnign up infinity goroutines, and instead only spin up infinity/2)
			default:
				//do it later
				go func() { getChan <- x }() //this is way less gross than the other blocking thing
			}

		}
	}

}
