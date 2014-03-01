package main

import (
    "fmt"
    "os"
    "time"
    "path"
    "path/filepath"
    "net/http"
    "log"
    "strings"
    "github.com/steeve/libtorrent-go"
)

const (
    VIRTUAL_READ_MAX_END_OFFSET = (100 * 1024) // if we read 100kb at the end of the file, virtual read
)

type TorrentFS struct {
    th      libtorrent.Torrent_handle
    ti      libtorrent.Torrent_info
}

type TorrentFile struct {
    tfs         *TorrentFS
    fe          libtorrent.File_entry
    fe_idx      int
    fp          *os.File
    stat        os.FileInfo
    dirFp       int
    virtualRead bool
}

func NewTorrentFS(th libtorrent.Torrent_handle) *TorrentFS {
    tfs := TorrentFS{th: th}
    go func() {
        for tfs.th.Status().GetHas_metadata() == false {
            time.Sleep(100 * time.Millisecond)
        }
        tfs.ti = tfs.th.Get_torrent_info()
    }()
    return &tfs
}

func (tfs *TorrentFS) ensureTorrentInfo() {
    for tfs.ti == nil {
        time.Sleep(100 * time.Millisecond)
    }
}

func (tfs *TorrentFS) TFSOpen(name string) (*TorrentFile, error) {
    log.Printf("Opening %s\n", name)
    tfs.ensureTorrentInfo()
    absPath, _ := filepath.Abs(tfs.th.Save_path())
    for i := 0; i < tfs.ti.Num_files(); i++ {
        fe := tfs.ti.File_at(i)
        feAbsPath, _ := filepath.Abs(path.Join(tfs.th.Save_path(), fe.GetPath()))
        if feAbsPath == absPath {
            return NewTorrentFile(tfs, feAbsPath)
        }
    }
    // In last resort, open file locally
    absFile, _ := filepath.Abs(path.Join(tfs.th.Save_path(), name))
    return NewTorrentFile(tfs, absFile)
}

func (tfs *TorrentFS) Open(name string) (http.File, error) {
    return tfs.TFSOpen(name)
}

func NewTorrentFile(tfs *TorrentFS, name string) (tf *TorrentFile, err error) {
    tf = &TorrentFile{tfs: tfs}

    fileAbsPath, _ := filepath.Abs(name)

    // Is this a file from the torrent ?
    // If so, permit opening before the file is effectively created
    for i := 0; i < tfs.ti.Num_files(); i++ {
        fe := tfs.ti.File_at(i)
        feAbsPath, _ := filepath.Abs(path.Join(tfs.th.Save_path(), fe.GetPath()))
        if feAbsPath == fileAbsPath {
            tf.fe = fe
            tf.fe_idx = i
            return
        }
    }

    tf.fp, err = os.Open(fileAbsPath)
    if err != nil {
        return
    }
    tf.stat, err = os.Stat(fileAbsPath)
    if err != nil {
        return
    }
    return
}

func (tf *TorrentFile) ensureFp() {
    if tf.fp == nil {
        fileAbsPath, _ := filepath.Abs(path.Join(tf.tfs.th.Save_path(), tf.fe.GetPath()))
        for {
            if _, ferr := os.Stat(fileAbsPath); ferr == nil {
                break
            }
            time.Sleep(100 * time.Millisecond)
        }
        tf.fp, _ = os.Open(fileAbsPath)
    }
}

func (tf *TorrentFile) Close() error {
    return tf.fp.Close()
}

func (tf *TorrentFile) Stat() (os.FileInfo, error) {
    return tf, nil
}

func (tf *TorrentFile) TFSReaddir(count int) (files []*TorrentFile, err error) {
    totalFiles := tf.tfs.ti.Num_files()
    files = make([]*TorrentFile, totalFiles - tf.dirFp)
    for ; tf.dirFp < totalFiles; tf.dirFp++ {
        files[tf.dirFp], err = NewTorrentFile(tf.tfs, path.Join(path.Join(tf.tfs.th.Save_path(), tf.tfs.ti.File_at(tf.dirFp).GetPath())))
    }
    return
}

func (tf *TorrentFile) Readdir(count int) (files []os.FileInfo, err error) {
    tfsfiles, err := tf.TFSReaddir(count)
    files = make([]os.FileInfo, len(tfsfiles))
    for i, file := range tfsfiles {
        files[i] = file
    }
    return
}

func (tf *TorrentFile) pieceFromOffset(offset int64) (int, int) {
    pieceLength := int64(tf.tfs.ti.Piece_length())
    piece := int((tf.Offset() + offset) / pieceLength)
    pieceOffset := int((tf.Offset() + offset) % pieceLength)
    return piece, pieceOffset
}

func (tf *TorrentFile) waitForPiece(piece int) {
    log.Printf("Waiting for piece %d\n", piece)
    for tf.tfs.th.Piece_priority(piece).(int) > 0 && tf.tfs.th.Have_piece(piece) == false {
        time.Sleep(100 * time.Millisecond)
    }
}

func (tf *TorrentFile) Read(data []byte) (int, error) {
    tf.ensureFp()

    // Dirty hack to ensure we don't need the last VIRTUAL_READ_MAX_END_OFFSET of a file to read it.
    if tf.virtualRead == true {
        log.Println("Virtual read.")
        tf.virtualRead = false
        return 0, nil
    }

    currentOffset, _ := tf.fp.Seek(0, os.SEEK_CUR)

    if len(data) <= tf.tfs.ti.Piece_length() {
        piece, _ := tf.pieceFromOffset(currentOffset + int64(len(data)))
        tf.waitForPiece(piece)
        return tf.fp.Read(data)
    }

    log.Println("Read more than one piece...")
    tmpData := make([]byte, tf.tfs.ti.Piece_length())
    read, err := tf.fp.Read(tmpData)
    if err != nil {
        return read, err
    }
    copy(data, tmpData[:read])
    return read, err
}

func (tf *TorrentFile) Seek(offset int64, whence int) (int64, error) {
    tf.ensureFp()

    // We are trying to read at the end of the file and we don't have the piece? Virtual read!
    if tf.Size() - offset < VIRTUAL_READ_MAX_END_OFFSET {
        piece, _ := tf.pieceFromOffset(offset)
        if tf.tfs.th.Have_piece(piece) == false {
            log.Printf("Virtual seek to %d\n", offset)
            tf.virtualRead = true
            return offset, nil
        }
    }

    // piece, _ := tf.pieceFromOffset(offset)
    // startPiece, endPiece := tf.Pieces()
    // for i := startPiece; i <= endPiece; i++ {
    //     if i < piece {
    //         tf.tfs.th.Piece_priority(i, 0)
    //     } else {
    //         tf.tfs.th.Piece_priority(i, 7)
    //     }
    // }

    return tf.fp.Seek(offset, whence)
}

// os.FileInfo
func (tf *TorrentFile) Name() string {
    if tf.fe != nil {
        return tf.fe.GetPath()
    }
    return tf.stat.Name()
}

func (tf *TorrentFile) Size() int64 {
    if tf.fe != nil {
        return tf.fe.GetSize()
    }
    return tf.stat.Size()
}

func (tf *TorrentFile) Mode() os.FileMode {
    return tf.stat.Mode()
}

func (tf *TorrentFile) ModTime() time.Time {
    if tf.fe != nil {
        return time.Unix(int64(tf.fe.GetMtime()), 0)
    }
    return tf.stat.ModTime()
}

func (tf *TorrentFile) IsDir() bool {
    if tf.fe != nil {
        return strings.HasSuffix(tf.Name(), "/")
    }
    return tf.stat.IsDir()
}

func (tf *TorrentFile) Sys() interface{} {
    return nil
}

// Specific to libtorrent
func (tf *TorrentFile) Offset() int64 {
    return tf.fe.GetOffset()
}

func (tf *TorrentFile) Pieces() (int, int) {
    startPiece, _ := tf.pieceFromOffset(1)
    endPiece, _ := tf.pieceFromOffset(tf.Size() - 1)
    return startPiece, endPiece
}

func (tf *TorrentFile) CompletedPieces() int {
    pieces := tf.tfs.th.Status().GetPieces()
    startPiece, endPiece := tf.Pieces()
    for i := startPiece; i <= endPiece; i++ {
        if pieces.Get_bit(i) == false {
            return i - startPiece
        }
    }
    return endPiece - startPiece
}

func (tf *TorrentFile) SetPriority(priority int) {
    log.Print("Setting priority %d to file %s\n", priority, tf.Name())
    tf.tfs.th.File_priority(tf.fe_idx, priority)
}

func (tf *TorrentFile) ShowPieces() {
    pieces := tf.tfs.th.Status().GetPieces()
    startPiece, endPiece := tf.Pieces()
    for i := startPiece; i <= endPiece; i++ {
        if pieces.Get_bit(i) == false {
            fmt.Printf("-")
        } else {
            fmt.Printf("#")
        }
    }
}
