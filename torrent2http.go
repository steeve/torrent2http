package main

import (
    "fmt"
    "io"
    "net/http"
    "time"
    "path"
    "os"
    // "log"
    "math"
    "encoding/hex"
    "encoding/json"
    "github.com/steeve/libtorrent-go"
)

type JSONStruct map[string]interface{}

func (r JSONStruct) String() (s string) {
    b, err := json.Marshal(r)
    if err != nil {
        s = ""
        return
    }
    s = string(b)
    return
}

var session libtorrent.Session
var torrentHandle libtorrent.Torrent_handle

func getPiecesForFile(f libtorrent.File_entry, pieceLength int) (int, int) {
    startPiece := int(f.GetOffset()) / pieceLength
    totalPieces := int(math.Ceil(float64(f.GetSize()) / float64(pieceLength)))
    return startPiece, startPiece + totalPieces
}

func getMaxPiece(pieces libtorrent.Bitfield, startPiece int, endPiece int) int {
    for i := startPiece; i <= endPiece; i++ {
        if pieces.Get_bit(i) == false {
            return i
        }
    }
    return pieces.Size()
}

func getBiggestFile(info libtorrent.Torrent_info) libtorrent.File_entry {
    retFile := info.File_at(0)
    for i := 1; i < info.Num_files(); i++ {
        if info.File_at(i).GetSize() > retFile.GetSize() {
            retFile = info.File_at(i)
        }
    }
    return retFile
}

func removeTorrent() {
    handle := torrentHandle
    torrentHandle = nil
    session.Remove_torrent(handle, 1);
}

func trackTorrentDownload(atp libtorrent.Add_torrent_params) {

}

func statusHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if torrentHandle == nil {
        fmt.Fprint(w, JSONStruct{"state": -1})
        return
    }
    status := torrentHandle.Status()
    fmt.Fprint(w, JSONStruct{
        "state": status.GetState(),
        "progress": status.GetProgress(),
        "download_rate": float32(status.GetDownload_rate()) / 1000,
        "upload_rate": float32(status.GetUpload_rate()) / 1000,
        "num_peers": status.GetNum_peers(),
        "num_seeds": status.GetNum_seeds(),
    })
}

func magnetHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        return
    }

    torrent_params := libtorrent.Parse_magnet_uri2(fmt.Sprintf("magnet:?%s", r.URL.RawQuery))
    torrent_params.SetStorage_mode(libtorrent.Storage_mode_allocate)
    hash := []byte(torrent_params.GetInfo_hash().To_string())
    fmt.Println(hex.EncodeToString(hash))

    new_hash := libtorrent.NewBig_number(torrent_params.GetInfo_hash().To_string())
    fmt.Println(session.Find_torrent(new_hash))

    torrentHandle = session.Add_torrent(torrent_params)
    torrentHandle.Set_sequential_download(true)

    fmt.Printf("Downloading: %s\n", torrent_params.GetName())

    fmt.Println("Waiting on metadata...")
    for torrentHandle.Status().GetHas_metadata() == false {
        time.Sleep(100 * time.Millisecond)
    }
    fmt.Println("Done.")

    torrentInfo := torrentHandle.Get_torrent_info()
    servedFile := getBiggestFile(torrentInfo)

    fmt.Printf("Video file is most likely to be: %s\n", servedFile.GetPath())
    fmt.Printf("Setting files priorities:\n")
    for i := 0; i < torrentInfo.Num_files(); i++ {
        if torrentInfo.File_at(i).GetPath() != servedFile.GetPath() {
            fmt.Printf("Setting 0 priority for %s\n", torrentInfo.File_at(i).GetPath())
            torrentHandle.File_priority(i, 0)
        }
    }


    startPiece, endPiece := getPiecesForFile(servedFile, torrentInfo.Piece_length())
    fmt.Printf("File is located between piece %d and %d\n", startPiece, endPiece)

    //w.Header().Set("Content-Length", fmt.Sprintf("%d", servedFile.GetSize()))

    lastPiece := 0
    currentPiece := 0
    pieceLength := torrentInfo.Piece_length()
    go func () {
        for torrentHandle != nil {
            s := torrentHandle.Status();

            fmt.Printf("\r%.2f%% complete (D:%.1fkb/s U:%.1fkB/s P:%d S:%d) Sent pieces: %d/%d",
                s.GetProgress() * 100,
                float32(s.GetDownload_rate()) / 1000,
                float32(s.GetUpload_rate()) / 1000,
                s.GetNum_peers(),
                s.GetNum_seeds(),
                currentPiece, torrentInfo.Num_pieces())

            time.Sleep(1 * time.Second)
        }
    }()

    fp, _ := os.Open(path.Join(torrentHandle.Save_path(), servedFile.GetPath()))
    defer fp.Close()

    for torrentHandle != nil {
        s := torrentHandle.Status()
        maxPiece := getMaxPiece(s.GetPieces(), startPiece, endPiece)

        for currentPiece = lastPiece; currentPiece < maxPiece; currentPiece++ {
            fp.Seek(int64(currentPiece * pieceLength), 0)
            _, err := io.CopyN(w, fp, int64(pieceLength))
            if err != nil {
                fmt.Println("\nClient disconnected, stopping!")
                removeTorrent()
                return
            }
        }
        if maxPiece == endPiece {
            removeTorrent()
            return
        }
        lastPiece = maxPiece

        time.Sleep(100 * time.Millisecond)
    }
}


func main() {
    fmt.Println("Starting BT engine...")
    session = libtorrent.NewSession()
    session.Listen_on(libtorrent.NewPair_int_int(6881, 6891))
    session.Start_dht()
    sessionSettings := session.Settings()
    sessionSettings.SetConnection_speed(500)
    sessionSettings.SetRequest_timeout(3)
    sessionSettings.SetPeer_connect_timeout(3)
    session.Set_settings(sessionSettings)
    fmt.Println("Started BT engine.")

    http.HandleFunc("/magnet:", magnetHandler)
    http.HandleFunc("/status", statusHandler)
    fmt.Println("Listening HTTP on port 5000")
    http.ListenAndServe(":5000", nil)
}
