package main

import (
    "fmt"
    "io"
    "net/http"
    "time"
    "path"
    "os"
    "os/signal"
    "syscall"
    "log"
    "flag"
    "math"
    "encoding/hex"
    "encoding/json"
    "./libtorrent-go"
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
var magnetUri string
var bindAddress string

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

func getBiggestFile(info libtorrent.Torrent_info) (int, libtorrent.File_entry) {
    idx := 0
    retFile := info.File_at(0)
    for i := 1; i < info.Num_files(); i++ {
        if info.File_at(i).GetSize() > retFile.GetSize() {
            idx = i
            retFile = info.File_at(i)
        }
    }
    return idx, retFile
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if torrentHandle == nil {
        fmt.Fprint(w, JSONStruct{"state": -1})
        return
    }
    status := torrentHandle.Status()

    maxPiece := 0
    totalPieces := 0
    if (status.GetHas_metadata()) {
        torrentInfo := torrentHandle.Get_torrent_info()
        _, servedFile := getBiggestFile(torrentInfo)
        startPiece, endPiece := getPiecesForFile(servedFile, torrentInfo.Piece_length())
        maxPiece = getMaxPiece(status.GetPieces(), startPiece, endPiece)
        totalPieces = endPiece - startPiece
    }

    fmt.Fprint(w, JSONStruct{
        "state": status.GetState(),
        "progress": status.GetProgress(),
        "download_rate": float32(status.GetDownload_rate()) / 1000,
        "upload_rate": float32(status.GetUpload_rate()) / 1000,
        "num_peers": status.GetNum_peers(),
        "num_seeds": status.GetNum_seeds(),
        "max_piece": maxPiece,
        "total_pieces": totalPieces,
    })
}

func fileStreamHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        return
    }

    // Make sure we first have metadata
    for torrentHandle.Status().GetHas_metadata() == false {
        time.Sleep(100 * time.Millisecond)
    }

    torrentInfo := torrentHandle.Get_torrent_info()
    log.Println(torrentInfo)
    servedFileIdx, servedFile := getBiggestFile(torrentInfo)
    // servedFileIdx, servedFile := findFileEntry(r.URL.Path)
    startPiece, endPiece := getPiecesForFile(servedFile, torrentInfo.Piece_length())

    // torrentHandle.File_priority(servedFileIdx, 6)
    for i := 0; i < torrentInfo.Num_files(); i++ {
        if i == servedFileIdx {
            torrentHandle.File_priority(i, 6)
        } else {
            torrentHandle.File_priority(i, 0)
        }
    }

    fp, _ := os.Open(path.Join(torrentHandle.Save_path(), servedFile.GetPath()))
    defer fp.Close()


    currentPiece := 0
    lastPiece := 0
    pieceLength := torrentInfo.Piece_length()
    for {
        s := torrentHandle.Status()
        maxPiece := getMaxPiece(s.GetPieces(), startPiece, endPiece)

        for currentPiece = lastPiece; currentPiece < maxPiece; currentPiece++ {
            fp.Seek(int64(currentPiece * pieceLength), 0)
            _, err := io.CopyN(w, fp, int64(pieceLength))
            if err != nil {
                log.Printf("Client disconnected from %s\n", r.URL.Path)
                return
            }
        }
        if maxPiece == endPiece {
            return
        }
        lastPiece = maxPiece

        time.Sleep(10 * time.Millisecond)
    }
}

func startServices() {
    log.Println("Starting DHT...")
    session.Start_dht()

    log.Println("Starting LSD...")
    session.Start_lsd()

    log.Println("Starting UPNP...")
    session.Start_upnp()

    log.Println("Starting NATPMP...")
    session.Start_natpmp()
}

func stopServices() {
    log.Println("Stopping DHT...")
    session.Stop_dht()

    log.Println("Stopping LSD...")
    session.Stop_lsd()

    log.Println("Stopping UPNP...")
    session.Stop_upnp()

    log.Println("Stopping NATPMP...")
    session.Stop_natpmp()
}

func removeFiles() {
    if torrentHandle.Status().GetHas_metadata() == false {
        return
    }

    torrentInfo := torrentHandle.Get_torrent_info()
    for i := 0; i < torrentInfo.Num_files(); i++ {
        os.RemoveAll(path.Join(torrentHandle.Save_path(), torrentInfo.File_at(i).GetPath()))
    }
}

func cleanup() {
    stopServices()

    log.Println("Removing torrent...")
    session.Set_alert_mask(libtorrent.AlertStorage_notification)

    // Just in case
    defer removeFiles()
    session.Remove_torrent(torrentHandle, 1);

    log.Println("Waiting for files to be removed...")
    for {
        if session.Wait_for_alert(libtorrent.Seconds(30)).Swigcptr() == 0 {
            return
        }
        if session.Pop_alert2().What() == "cache_flushed_alert" {
            return
        }
    }
}

func parseFlags() {
    flag.StringVar(&magnetUri, "magnet", "", "Magnet URI of Torrent")
    flag.StringVar(&bindAddress, "bind", ":5001", "Bind address of torrent2http2")
    flag.Parse()

    if magnetUri == "" {
        flag.Usage();
        os.Exit(1)
    }
}

func main() {
    parseFlags()

    log.Println("Starting BT engine...")
    session = libtorrent.NewSession()
    session.Listen_on(libtorrent.NewPair_int_int(6881, 6891))

    log.Println("Setting Session settings...")
    sessionSettings := session.Settings()
    sessionSettings.SetConnection_speed(500)
    sessionSettings.SetRequest_timeout(3)
    sessionSettings.SetPeer_connect_timeout(3)
    session.Set_settings(sessionSettings)

    startServices()

    magnetUri := flag.String("magnet", "", "Magnet URI of Torrent")
    flag.Parse()

    torrentParams := libtorrent.Parse_magnet_uri2(*magnetUri)
    torrentHash := []byte(torrentParams.GetInfo_hash().To_string())
    torrentParams.SetStorage_mode(libtorrent.Storage_mode_allocate)
    torrentHandle = session.Add_torrent(torrentParams)
    torrentHandle.Set_sequential_download(true)
    log.Printf("Downloading: %s (%s)\n", torrentParams.GetName(), hex.EncodeToString(torrentHash))

    log.Println("Registering HTTP endpoints...")
    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/file", fileStreamHandler)

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func(){
        <-c
        log.Println("Stopping torrent2http...")
        cleanup()
        log.Println("Bye bye")
        os.Exit(0)
    }()

    log.Println("Listening HTTP on port 5000...")
    http.ListenAndServe(":5000", nil)
}
