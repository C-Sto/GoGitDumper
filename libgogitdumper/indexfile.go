package libgogitdumper

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
)

func ReadIndex(b []byte) (IndexFile, error) {
	return IndexFile{}, nil
}

func ParseIndexFile(b []byte) (IndexFile, error) {
	// thanks to this guy https://github.com/sbp/gin/blob/master/gin
	indx := IndexFile{}
	readcount := uint16(0) //easier this way
	//4 byte signature "DIRC"
	readcount += 4
	if string(b[:readcount]) != "DIRC" {
		return IndexFile{}, errors.New("Bad index file")
	}
	indx.Signature = string(b[:readcount])

	//4 byte version number (32bit int)
	indx.Version = binary.BigEndian.Uint32(b[readcount : readcount+4])
	if indx.Version != 2 && indx.Version != 3 {
		return IndexFile{}, errors.New("Bad index file")
	}
	readcount += 4

	//4 byte count of index entries
	indx.EntryCount = binary.BigEndian.Uint32(b[readcount : readcount+4])
	readcount += 4

	//for each entry
	for x := uint32(1); x <= indx.EntryCount; x++ {
		if uint16(64)+readcount > uint16(len(b)) {
			continue
		}
		entryLen := uint16(0)
		entry := IndexEntry{}
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
				return IndexFile{}, errors.New("Index entry padding error")
			}
		}

		indx.Entries = append(indx.Entries, entry)
	}

	return indx, nil
}
