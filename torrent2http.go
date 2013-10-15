package main

import (
    "fmt"
    "net/http"
    "path"
    "os"
    "os/signal"
    "syscall"
    "log"
    "flag"
    "math"
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

type Config struct {
    magnetUri           string
    bindAddress         string
    max_upload_rate     int
    max_download_rate   int
    download_path       string
    keep_file           bool
    min_memory_mode     bool
}

var config Config
var session libtorrent.Session
var torrentHandle libtorrent.Torrent_handle
var tfs *TorrentFS

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
        "num_seeds": status.GetNum_seeds()})
}

func lsHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    dir, _ := tfs.TFSOpen("/")
    files, _ := dir.TFSReaddir(-1)
    retFiles := make([]JSONStruct, len(files))
    for i, file := range files {
        startPiece, endPiece := file.Pieces()
        retFiles[i] = JSONStruct{
            "name": file.Name(),
            "size": file.Size(),
            "offset": file.Offset(),
            "total_pieces": int(math.Max(float64(endPiece - startPiece), 1)),
            "complete_pieces": file.CompletedPieces()}
    }

    fmt.Fprint(w, JSONStruct{"files": retFiles})
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

    if config.keep_file == true {
        return
    }

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
    if config.min_memory_mode == true {
        sessionSettings = libtorrent.Min_memory_usage()
        sessionSettings.SetMax_queued_disk_bytes(64 * 1024)
    }
    sessionSettings.SetConnection_speed(1000)
    sessionSettings.SetRequest_timeout(1)
    sessionSettings.SetPeer_connect_timeout(1)
    if config.max_download_rate > 0 {
        sessionSettings.SetDownload_rate_limit(80 * 1024)
    }
    if config.max_upload_rate > 0 {
        sessionSettings.SetUpload_rate_limit(config.max_upload_rate * 1024)
    }
    session.Set_settings(sessionSettings)

    startServices()

    torrentParams := libtorrent.Parse_magnet_uri2(config.magnetUri)
    torrentParams.SetSave_path(config.download_path)
    torrentHandle = session.Add_torrent(torrentParams)
    torrentHandle.Set_sequential_download(true)
    log.Printf("Downloading: %s\n", torrentParams.GetName())

    tfs = NewTorrentFS(torrentHandle)

    log.Println("Registering HTTP endpoints...")
    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/ls", lsHandler)
    http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(tfs)))

    // Shutdown procedures
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    // Allow shutdown via HTTP
    http.HandleFunc("/shutdown", func (w http.ResponseWriter, r *http.Request) {
        c <- os.Interrupt
    })
    go func(){
        <-c
        log.Println("Stopping torrent2http...")
        cleanup()
        log.Println("Bye bye")
        os.Exit(0)
    }()

    log.Printf("Listening HTTP on %s...\n", config.bindAddress)
    http.ListenAndServe(config.bindAddress, nil)
}
