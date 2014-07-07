package filesystem

import (
	"fmt"
	"os"
	"path"
)

//IsDir returns true if the File is a directory.
func IsDir(file *os.File) (bool, error) {
	st, err := file.Stat()
	if err != nil {
		return false, err
	}
	return st.IsDir(), err
}

//DirectoryEntry is an entry for a directory listing. It's meant to be easy to
//turn into JSON.
type DirectoryEntry struct {
	Path         string      //Path to the file relative to the working directory.
	Mode         os.FileMode //Permissions
	DateModified int64       //Marshalled version of a Date.
	Size         int64       //The file size in bytes.
	Type         string      //Either "File" or "Folder"
}

//DirectoryListing contains all of the objects returned in a directory.
type DirectoryListing struct {
	Listing []*DirectoryEntry
}

//NewDirectoryListing returns a pointer to a newly created DirectoryListing.
func NewDirectoryListing() *DirectoryListing {
	return &DirectoryListing{
		Listing: make([]*DirectoryEntry, 0),
	}
}

//FileInfoToEntry transforms a FileInfo object into a DirectoryEntry.
func FileInfoToEntry(dir string, info os.FileInfo) *DirectoryEntry {
	path := path.Join(dir, info.Name())
	mode := info.Mode()
	date := info.ModTime().Unix()
	size := info.Size()
	var entrytype string
	if info.IsDir() {
		entrytype = "Folder"
	} else {
		entrytype = "File"
	}
	entry := &DirectoryEntry{
		Path:         path,
		Mode:         mode,
		DateModified: date,
		Size:         size,
		Type:         entrytype,
	}
	return entry
}

//ListDir returns a filled in instance of DirectoryListing based on the contents
//of 'base'.
func ListDir(base string) (*DirectoryListing, error) {
	dirFile, err := os.Open(base)
	if err != nil {
		return nil, err
	}
	dirCheck, err := IsDir(dirFile)
	if err != nil {
		return nil, err
	}
	if !dirCheck {
		return nil, fmt.Errorf("%s is not a directory", base)
	}
	infoResults, err := dirFile.Readdir(0)
	if err != nil {
		return nil, err
	}
	listing := NewDirectoryListing()
	for _, info := range infoResults {
		entry := FileInfoToEntry(base, info)
		listing.Listing = append(listing.Listing, entry)
	}
	return listing, err
}
