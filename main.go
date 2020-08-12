package main

import (
	"bytes"
	"compress/zlib"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	urlpkg "net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	//_ "net/http/pprof"
	//"./libgogitdumper"
	"github.com/c-sto/gogitdumper/libgogitdumper"
)

var version = "0.6.0"

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

var fileCount uint64
var byteCount uint64

var client *http.Client

func printBanner() {
	//todo: include settings in banner
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
	flag.StringVar(&cfg.Localpath, "o", ".git"+string(os.PathSeparator), "Local folder to dump into")
	flag.BoolVar(&cfg.IndexBypass, "i", false, "Bypass parsing the index file, but still download it")
	flag.StringVar(&cfg.IndexLocation, "l", "", "Location of a local index file to parse instead of getting it using this tool")
	flag.BoolVar(&SSLIgnore, "k", false, "Ignore SSL check")
	flag.StringVar(&cfg.ProxyAddr, "p", "", "Proxy configuration options in the form ip:port eg: 127.0.0.1:9050")
	force := flag.Bool("f", false, "force overwrite of .git dir")
	flag.Parse()

	if _, err := os.Stat(cfg.Localpath); !os.IsNotExist(err) && !*force {
		//exists
		fmt.Println("directory exists!!! do you want to overwrite?? if yes, run again with -f")
		os.Exit(1)
	}

	if cfg.Url == "" { //todo: check for correct .git thing
		panic("Url required")
	}
	httpTransport := &http.Transport{}
	client = &http.Client{Transport: httpTransport}

	//skip ssl errors if requested to
	httpTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: SSLIgnore}
	//http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: SSLIgnore}

	//use a proxy if requested to
	if cfg.ProxyAddr != "" { // proxy configured, in burpmode, and at the stage where we want to actually send it to burp
		if strings.HasPrefix(cfg.ProxyAddr, "http") {
			proxyURL, err := urlpkg.Parse(cfg.ProxyAddr)
			if err != nil {
				panic(err)
			}
			httpTransport.Proxy = http.ProxyURL(proxyURL)
			//test proxy
			_, err = net.Dial("tcp", proxyURL.Host)
			if err != nil {
				panic(err)
			}
		} else {
			dialer, err := proxy.SOCKS5("tcp", cfg.ProxyAddr, nil, proxy.Direct)
			if err != nil {
				panic(err)
			}
			httpTransport.Dial = dialer.Dial
			//test proxy
			_, err = net.Dial("tcp", cfg.ProxyAddr)
			if err != nil {
				panic(err)
			}
		}
	}

	workers := cfg.Threads
	tested = libgogitdumper.ThreadSafeSet{}.Init()

	wg := &sync.WaitGroup{} //this is way overcomplicate, there is probably a better way...

	url = cfg.Url
	localpath = cfg.Localpath

	//setting the chan size to bigger than the number of workers to avoid deadlocks on high worker counts
	getqueue := make(chan string, workers*2)
	newfilequeue := make(chan string, workers*2)
	writefileChan := make(chan libgogitdumper.Writeme, workers*2)

	go libgogitdumper.LocalWriter(writefileChan, localpath, &fileCount, &byteCount, wg) //writes out the downloaded files

	//takes any new objects identified, and checks to see if already downloaded. will add new files to the queue if unique.
	go adderWorker(getqueue, newfilequeue, wg)

	isListingEnabled, rawListing := testListing(url)

	if isListingEnabled {
		fmt.Println("Indexing identified, recursively downloading repo directory...")
		for x := 0; x < workers; x++ {
			go ListingGetWorker(getqueue, newfilequeue, writefileChan, wg)
		}
		for _, x := range parseListing(rawListing) {
			wg.Add(1)
			newfilequeue <- url + x
		}
	} else {
		//downloader bois
		for x := 0; x < workers; x++ {
			go GetWorker(getqueue, newfilequeue, writefileChan, wg)
		}

		//get the index file, parse it for files and whatnot
		if cfg.IndexBypass {
			wg.Add(1)
			newfilequeue <- url + "index"
		} else if cfg.IndexLocation != "" {
			indexfile, err := ioutil.ReadFile(cfg.IndexLocation)
			if err != nil {
				panic("Could not read index file: " + err.Error())
			}
			err = getIndex(indexfile, newfilequeue, writefileChan, wg)
			if err != nil {
				panic(err)
			}
		} else {
			indexfile, err := libgogitdumper.GetThing(url+"index", client)
			if err != nil {
				panic(err)
			}

			err = getIndex(indexfile, newfilequeue, writefileChan, wg)
			if err != nil {
				panic(err)
			}
		}

		//get the packs (if any exist) and parse them out too
		getPacks(newfilequeue, writefileChan, wg)

		//get all the common things that contain refs
		for _, x := range commonrefs {
			wg.Add(1)
			newfilequeue <- url + x
		}

		//get all the common files that may be important I guess?
		for _, x := range commonfiles {
			wg.Add(1)
			newfilequeue <- url + x
		}
	}

	wg.Wait() //this is more accurate, but difficult to manage and makes the code all gross(er)

	//keeping this here for legacy - it should always break out
	for {
		if len(getqueue) == 0 && len(newfilequeue) == 0 {
			break
		}
		fmt.Println("ERROR! WG CALCULATION WRONG")
		time.Sleep(time.Second * 2)
	}
	fmt.Printf("Wrote %d files and %d bytes", fileCount, byteCount)

}

func parseListing(page []byte) []string {
	var r []string
	baseDirRe := regexp.MustCompile("Directory listing for /.git/.*<")
	baseDirByt := baseDirRe.Find(page)
	baseDirStr := string(baseDirByt[28 : len(baseDirByt)-1])
	listingRe := regexp.MustCompile("href=[\"'](.*?)[\"']")
	match := listingRe.FindAll(page, -1)
	for _, x := range match {
		r = append(r, baseDirStr+string(x[6:len(x)-1]))
	}
	return r
}

func getPacks(newfilequeue chan string, writefileChan chan libgogitdumper.Writeme, wg *sync.WaitGroup) {
	//todo: parse packfiles for new objects and whatnot
	//get packfiles from objects/info/packs

	packfile, err := libgogitdumper.GetThing(url+"objects/info/packs", client)
	if err != nil {
		//handle error?
	}
	fmt.Println("Downloaded: ", url+"objects/info/packs")

	d := libgogitdumper.Writeme{}
	d.LocalFilePath = localpath + string(os.PathSeparator) + "objects" + string(os.PathSeparator) + "info" + string(os.PathSeparator) + "packs"
	d.Filecontents = packfile

	wg.Add(1)
	writefileChan <- d

	if len(packfile) > 0 {
		//this is not how packfiles work. Worst case is we accidentally download some packfiles,
		//but as the sha1 is based on the last 20 bytes (or something like that), not sure how to do this blindly
		sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
		match := sha1re.FindAll(packfile, -1) //doing dumb regex look for sha1's in packfiles, I don't think this is how it works tbh
		for _, x := range match {

			wg.Add(1)
			newfilequeue <- url + "objects/pack/pack-" + string(x) + ".idx"
			wg.Add(1)
			newfilequeue <- url + "objects/pack/pack-" + string(x) + ".pack"
		}

	}
}

func getIndex(indexfile []byte, newfileChan chan string, localfileChan chan libgogitdumper.Writeme, wg *sync.WaitGroup) error {

	fmt.Println("Downloaded: ", url+"index")

	d := libgogitdumper.Writeme{}
	d.LocalFilePath = localpath + string(os.PathSeparator) + "index"
	d.Filecontents = indexfile

	wg.Add(1)
	localfileChan <- d

	parsed, err := libgogitdumper.ParseIndexFile(indexfile)
	if err != nil {
		//deal with parsing error X_X (not blocking for now)
		return nil
	}

	for _, x := range parsed.Entries {
		wg.Add(1)
		newfileChan <- url + "objects/" + string(x.Sha1[0:2]) + "/" + string(x.Sha1[2:])
	}

	return err

}

func testListing(url string) (bool, []byte) {
	resp, err := libgogitdumper.GetThing(url, client)
	if err != nil {
		fmt.Println(err, "\nError during indexing test")
		return false, nil
		//todo: handle err better
	}

	if strings.Contains(string(resp), "<title>Directory listing for ") {
		return true, resp
	}
	return false, nil
}

func ListingGetWorker(c chan string, c2 chan string, localFileWriteChan chan libgogitdumper.Writeme, wg *sync.WaitGroup) {
	for {
		path := <-c
		//check for directory
		if string(path[len(path)-1]) == "/" {
			//don't bother downloading this file to save locally, but parse it for MORE files!
			isActually, listingContent := testListing(path)
			if isActually {
				fmt.Println("Found Directory: ", path)
				for _, x := range parseListing(listingContent) {
					wg.Add(1) //to be processed by adderworker
					c2 <- url + x
				}
			}

		} else {
			//not a directory, download the file and write it as per normal
			resp, err := libgogitdumper.GetThing(path, client)
			if err != nil {
				fmt.Println(err, path)
				wg.Done()
				continue //todo: handle err better
			}
			fmt.Println("Downloaded: ", path)
			//write to local path
			d := libgogitdumper.Writeme{}
			d.LocalFilePath = localpath + string(os.PathSeparator) + path[len(url):]
			d.Filecontents = resp

			wg.Add(1) //to be processed by localwriterworker
			localFileWriteChan <- d
		}
		wg.Done() //finished getting the new thing
	}
}

func GetWorker(c chan string, c2 chan string, localFileWriteChan chan libgogitdumper.Writeme, wg *sync.WaitGroup) {
	sha1re := regexp.MustCompile("[0-9a-fA-F]{40}")
	refre := regexp.MustCompile(`(refs(/[a-zA-Z0-9\-\.\_\*]+)+)`)
	for {
		path := <-c
		resp, err := libgogitdumper.GetThing(path, client)
		if err != nil {
			fmt.Println(err, path)
			wg.Done()
			continue //todo: handle err better
		}
		fmt.Println("Downloaded: ", path)
		if strings.Contains(path, "/objects/") && !bytes.HasPrefix(resp, []byte{120, 1}) {
			//all object files have to be gz
			fmt.Println(resp[:5], []byte{120, 1}, string(resp[:5]))
			wg.Done()
			continue
		}
		//write to local path
		d := libgogitdumper.Writeme{}
		d.LocalFilePath = localpath + string(os.PathSeparator) + path[len(url):]
		d.Filecontents = make([]byte, len(resp))
		copy(d.Filecontents, resp)

		wg.Add(1)
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
		if bytes.HasPrefix(resp, []byte("tree")) {
			treeobj := libgogitdumper.ParseTreeFile(resp)
			for _, x := range treeobj.TreeEntries {
				//add sha1's to line
				sha1string := fmt.Sprintf("%x", x.Hash)
				wg.Add(1)
				c2 <- url + "objects/" + string(sha1string[0:2]) + "/" + string(sha1string[2:])

			}

		}
		match := sha1re.FindAll(resp, -1)
		for _, x := range match {
			//add sha1's to line
			wg.Add(1)
			c2 <- url + "objects/" + string(x[0:2]) + "/" + string(x[2:])

		}

		//check for ref paths in the thing
		match = refre.FindAll(resp, -1)
		for _, x := range match {
			if string(x[len(x)-1]) == "*" {
				continue
			}
			wg.Add(1)
			c2 <- url + string(x)
			wg.Add(1)
			c2 <- url + "logs/" + string(x)
		}
		wg.Done()

	}
}

func adderWorker(getChan chan string, potentialChan chan string, wg *sync.WaitGroup) {
	for {
		x := <-potentialChan
		if !tested.HasValue(x) {
			tested.Add(x)
			wg.Add(1) //signal that we have some more stuff to do (added to the 'get' chan)
			select {
			case getChan <- x:
				//do nothing (this should avoid spinnign up infinity goroutines, and instead only spin up infinity/2)
			default:
				//do it later
				go func() { getChan <- x }() //this is way less gross than the other blocking thing
			}

		}
		wg.Done() //we finished processing the potentially new thing
	}

}
