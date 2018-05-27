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
If directory listing is not enabled:

It may not collect every single bit of the source repo - but it should get a substantial amount of it fairly quickly.

It definitely won't get most of the packfiles, which is very annoying, but I can't think of a good method of obtaining those yet.

If directory listing is enabled:
it should get everything. Congrats, you have the full source of the target.


### Description

More than once during an engagement I've needed to dump a public git repo that is available over http, occasionally without directory indexing enabled.

This tool can be pointed at a git repo hosted on a web server, it will attempt to mirror it using whatever method is best. If directory indexing is detected, it will mirror all of the files in the .git/ directory. If indexing is not enabled, the tool will parse the .git/index file for references, then recursively check all references for more references etc.

If files are being obtained via references in the index file, packfiles will probably be missed. At this stage, I'm unsure if there is a good way of getting those files blindly. If you have an idea for this, please let me know.

The current tools available for this are relatively slow - who has time to wait 30 seconds for a repo do dump? not me, that's for sure.

As with any code I write - it's likely awful, full of bugs etc. This is an attempt to make my life easier - if you feel the code should work differently, or could be better, please either:
- Tell me. I'll probably agree and implement your suggestions if I have time.
- Submit an Issue. I'll probably agree and implement your suggestions if I have time.
- Submit a pull request. I'll probably like it, and merge it if it's not too terrible.

Code based heavily on https://github.com/arthaud/git-dumper/ and https://github.com/internetwache/GitTools which are reasonably good tools I've found to help with this process. Index parsing mostly based on https://github.com/sbp/gin/ 
