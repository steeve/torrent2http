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
    keep_files          bool
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

    if config.keep_files == true {
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
    config = Config{}
    flag.StringVar(&config.magnetUri, "magnet", "", "Magnet URI")
    flag.StringVar(&config.bindAddress, "bind", ":5001", "Bind address of torrent2http2")
    flag.IntVar(&config.max_download_rate, "dlrate", 0, "Max Download Rate")
    flag.IntVar(&config.max_upload_rate, "ulrate", 0, "Max Upload Rate")
    flag.StringVar(&config.download_path, "dlpath", ".", "Download path")
    flag.BoolVar(&config.keep_files, "keep", false, "Keep files after exiting")
    flag.BoolVar(&config.min_memory_mode, "minmem", false, "Min memory mode (for embedded platforms such as Raspberry Pi)")
    flag.Parse()

    if config.magnetUri == "" {
        flag.Usage();
        os.Exit(1)
    }
}

// Go version of libtorrent.Min_memory_usage in case we need to tweak
func getMimMemorySettings() libtorrent.Session_settings {
    set := session.Settings()

    set.SetAlert_queue_size(100);

    // setting this to a low limit, means more
    // peers are more likely to request from the
    // same piece. Which means fewer partial
    // pieces and fewer entries in the partial
    // piece list
    set.SetWhole_pieces_threshold(2)
    set.SetUse_parole_mode(false)
    set.SetPrioritize_partial_pieces(true)

    // connect to 5 peers per second
    set.SetConnection_speed(5)

    // be extra nice on the hard drive when running
    // on embedded devices. This might slow down
    // torrent checking
    set.SetFile_checks_delay_per_block(5)

    // only have 4 files open at a time
    set.SetFile_pool_size(4)

    // we want to keep the peer list as small as possible
    set.SetAllow_multiple_connections_per_ip(false)
    set.SetMax_failcount(2)
    set.SetInactivity_timeout(120)

    // whenever a peer has downloaded one block, write
    // it to disk, and don't read anything from the
    // socket until the disk write is complete
    set.SetMax_queued_disk_bytes(1)

//===================================================================

    // don't keep track of all upnp devices, keep
    // the device list small
    set.SetUpnp_ignore_nonrouters(true)

    // never keep more than one 16kB block in
    // the send buffer
    set.SetSend_buffer_watermark(9)

    // don't use any disk cache
    // set.SetCache_size(0)
    // set.SetCache_buffer_chunk_size(1)
    // set.SetUse_read_cache(false)
    // set.SetUse_disk_read_ahead(false)

    set.SetClose_redundant_connections(true)

    set.SetMax_peerlist_size(500)
    set.SetMax_paused_peerlist_size(50)

    // udp trackers are cheaper to talk to
    set.SetPrefer_udp_trackers(true)

    set.SetMax_rejects(10)

    set.SetRecv_socket_buffer_size(16 * 1024)
    set.SetSend_socket_buffer_size(16 * 1024)

    // use less memory when checking pieces
    // set.SetOptimize_hashing_for_speed(false)

    // use less memory when reading and writing
    // whole pieces
    set.SetCoalesce_reads(false)
    set.SetCoalesce_writes(false)

    // disallow the buffer size to grow for the uTP socket
    set.SetUtp_dynamic_sock_buf(false)

    return set
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
