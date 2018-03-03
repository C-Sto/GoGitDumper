package libgogitdumper

type Writeme struct {
	LocalFilePath string
	Filecontents  []byte
}

type Config struct {
	Threads       int
	Url           string
	Localpath     string
	IndexBypass   bool
	IndexLocation string
}

type IndexFile struct {
	Signature  string //should be "DIRC"
	Version    uint32
	EntryCount uint32
	Entries    []IndexEntry
}

type IndexEntry struct {
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

	Ext_signature [4]byte //don't care about the data being totally correct, as long as we get semi good values out of it
	Ext_size      [4]byte //32bit int

}

//the actual .pack file
type PackFile struct {
	//first 12 bytes are meta-info
	Header      [4]byte //should be 'PACK'
	Version     [4]byte //version - probably 0,0,0,2 or someth
	ObjectCount [4]byte //count of all objects in the file

	//last 20 bytes are a checksum
	checksum [20]byte
}

type PackfileObjects struct {
}

type PackIndex struct {
	//first 8 bytes is header
	Header  [4]byte //should be 255,116,79,99
	Version [4]byte //should be 0,0,0,2

}
