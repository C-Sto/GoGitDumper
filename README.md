# GoGitDumper 

### Installation

```
go get github.com/c-sto/gogitdumper
```


### Usage

```
gogitdumper -u http://urlhere.com/.git/ -o yourdecideddir/.git/
..(wait for dumping)..
cd yourdecdieddir
git log
git checkout *
```

It may not collect every single bit of the source repo - but it should get a substantial amount of it fairly quickly.

It definitely won't get most of the packfiles, which is very annoying, but I can't think of a good method of obtaining those yet.


### Description

More than once during an engagement I've needed to dump a public git repo that is available over http, without directory indexing enabled. With driectory indexing enabled, it's significantly easier - probably just recursive wget in those instances (or maybe I'll implement indexing logic eventually, but I cbf at this stage).

The current tools available for this are relatively slow - who has time to wait 30 seconds for a repo do dump? not me, that's for sure.

As with any code I write - it's likely awful, full of bugs etc. This is an attempt to make my life easier - if you feel the code should work differently, or could be better, please either:
- Tell me. I'll probably agree and implement your suggestions if I have time.
- Submit an Issue. I'll probably agree and implement your suggestions if I have time.
- Submit a pull request. I'll probably like it, and merge it if it's not too terrible.

Code based heavily on https://github.com/arthaud/git-dumper/ and https://github.com/internetwache/GitTools which are reasonably good tools I've found to help with this process. Index parsing mostly based on https://github.com/sbp/gin/ 
