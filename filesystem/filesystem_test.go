package filesystem

import (
	"os"
	"path"
	"testing"
)

func TestIsDir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf(err.Error())
	}
	wdFile, err := os.Open(wd)
	if err != nil {
		t.Errorf(err.Error())
	}
	dirBool, err := IsDir(wdFile)
	if err != nil {
		t.Errorf(err.Error())
	}
	if !dirBool {
		t.Errorf("Working directory isn't a directory?")
	}
}

func TestNewDirectoryListing(t *testing.T) {
	l := NewDirectoryListing()
	if l == nil {
		t.Fail()
	}
}

func TestFileInfoToEntry(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf(err.Error())
	}
	srcPath := path.Join(wd, "filesystem.go")
	srcFile, err := os.Open(srcPath)
	if err != nil {
		t.Errorf(err.Error())
	}
	srcInfo, err := srcFile.Stat()
	if err != nil {
		t.Errorf(err.Error())
	}
	srcEntry := FileInfoToEntry(wd, srcInfo)
	if srcEntry.Path != srcPath {
		t.Errorf("%s does not match %s", srcEntry.Path, srcPath)
	}
	if srcEntry.Size <= 0 {
		t.Errorf("Size is <= 0: %d", srcEntry.Size)
	}
	if srcEntry.Type == "Folder" {
		t.Errorf("Type is set to 'Folder'")
	}
	if srcEntry.Mode != 0644 {
		t.Errorf("Permissions were not set to 0644")
	}
}

func TestListDir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf(err.Error())
	}
	listing, err := ListDir(wd)
	if err != nil {
		t.Errorf(err.Error())
	}
	if len(listing.Listing) == 0 {
		t.Errorf("Length of the ListDir results was 0.")
	}
}
