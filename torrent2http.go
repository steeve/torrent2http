package main

import (
    "net/http"
    "path"
    "os"
    "os/signal"
    "syscall"
    "log"
    "flag"
    "math"
    "encoding/json"
    "github.com/steeve/libtorrent-go"
    "runtime"
    "fmt"
    "time"
)

type FileStatusInfo struct {
    Name            string  `json:"name"`
    Size            int64   `json:"size"`
    Offset          int64   `json:"offset"`
    TotalPieces     int     `json:"total_pieces"`
    CompletePieces  int     `json:"complete_pieces"`
}

type LsInfo struct {
    Files   []FileStatusInfo    `json:"files"`
}

type SessionStatus struct {
    State           int     `json:"state"`
    Progress        float32 `json:"progress"`
    DownloadRate    float32 `json:"download_rate"`
    UploadRate      float32 `json:"upload_rate"`
    NumPeers        int     `json:"num_peers"`
    NumSeeds        int     `json:"num_seeds"`
}

type Config struct {
    magnetUri           string
    bindAddress         string
    maxUploadRate       int
    maxDownloadRate     int
    downloadPath        string
    keepFiles           bool
    encryption          int
    noSparseFile        bool
}

type Instance struct {
    config           Config
    session          libtorrent.Session
    torrentHandle    libtorrent.Torrent_handle
    torrentFS        *TorrentFS
}

var instance = Instance{}
var mainFuncChan = make(chan func())

func runInMainThread(f interface{}) interface{} {
    done := make(chan interface{}, 1)
    mainFuncChan <- func() {
        switch f := f.(type) {
        case func():
            f()
            done <- true
        case func() interface{}:
            done <- f()
        }
    }
    return <-done
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    var status SessionStatus
    if instance.torrentHandle == nil {
        status = SessionStatus{State: -1}
    } else {
        runInMainThread(func() {
            tstatus := instance.torrentHandle.Status()
            status = SessionStatus{
                State: int(tstatus.GetState()),
                Progress: tstatus.GetProgress(),
                DownloadRate: float32(tstatus.GetDownload_rate()) / 1000,
                UploadRate: float32(tstatus.GetUpload_rate()) / 1000,
                NumPeers: tstatus.GetNum_peers(),
                NumSeeds: tstatus.GetNum_seeds()}
        })
    }

    output, _ := json.Marshal(status)
    w.Write(output)
}

func lsHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    dir, _ := instance.torrentFS.TFSOpen("/")
    files, _ := dir.TFSReaddir(-1)
    retFiles := LsInfo{}

    for _, file := range files {
        startPiece, endPiece := file.Pieces()

        fi := FileStatusInfo{
            Name: file.Name(),
            Size: file.Size(),
            Offset: file.Offset(),
            TotalPieces: int(math.Max(float64(endPiece - startPiece), 1)),
            CompletePieces: file.CompletedPieces()}
        retFiles.Files = append(retFiles.Files, fi)
    }

    output, _ := json.Marshal(retFiles)
    w.Write(output)
}

func startServices() {
    log.Println("Starting DHT...")
    instance.session.Start_dht()

    log.Println("Starting LSD...")
    instance.session.Start_lsd()

    log.Println("Starting UPNP...")
    instance.session.Start_upnp()

    log.Println("Starting NATPMP...")
    instance.session.Start_natpmp()
}

func stopServices() {
    log.Println("Stopping DHT...")
    instance.session.Stop_dht()

    log.Println("Stopping LSD...")
    instance.session.Stop_lsd()

    log.Println("Stopping UPNP...")
    instance.session.Stop_upnp()

    log.Println("Stopping NATPMP...")
    instance.session.Stop_natpmp()
}

func removeFiles() {
    if instance.torrentHandle.Status().GetHas_metadata() == false {
        return
    }

    torrentInfo := instance.torrentHandle.Get_torrent_info()
    for i := 0; i < torrentInfo.Num_files(); i++ {
        os.RemoveAll(path.Join(instance.torrentHandle.Save_path(), torrentInfo.File_at(i).GetPath()))
    }
}

func shutdown() {
    runInMainThread(func () {
        log.Println("Stopping torrent2http...")

        stopServices()

        log.Println("Removing torrent...")

        if instance.config.keepFiles == false {
            instance.session.Set_alert_mask(libtorrent.AlertStorage_notification)
            // Just in case
            defer removeFiles()
            instance.session.Remove_torrent(instance.torrentHandle, 1);
            log.Println("Waiting for files to be removed...")
            for {
                if instance.session.Wait_for_alert(libtorrent.Seconds(30)).Swigcptr() == 0 {
                    break
                }
                if instance.session.Pop_alert2().What() == "cache_flushed_alert" {
                    break
                }
            }
        }

        log.Println("Bye bye")
        os.Exit(0)
    })
}

func parseFlags() {
    config := Config{}
    flag.StringVar(&config.magnetUri, "magnet", "", "Magnet URI")
    flag.StringVar(&config.bindAddress, "bind", ":5001", "Bind address of torrent2http")
    flag.IntVar(&config.maxDownloadRate, "dlrate", 0, "Max Download Rate")
    flag.IntVar(&config.maxUploadRate, "ulrate", 0, "Max Upload Rate")
    flag.StringVar(&config.downloadPath, "dlpath", ".", "Download path")
    flag.BoolVar(&config.keepFiles, "keep", false, "Keep files after exiting")
    flag.BoolVar(&config.noSparseFile, "no-sparse", false, "Do not use sparse file allocation.")
    flag.IntVar(&config.encryption, "encryption", 1, "Encryption: 0=forced 1=enabled (default) 2=disabled")
    flag.Parse()

    if config.magnetUri == "" {
        flag.Usage();
        os.Exit(1)
    }

    instance.config = config
}

func configureSession() {
    settings := instance.session.Settings()

    log.Println("Setting Session settings...")
    settings.SetConnection_speed(1000)
    settings.SetRequest_timeout(5)
    settings.SetPeer_connect_timeout(2)
    settings.SetAnnounce_to_all_trackers(true);
    settings.SetAnnounce_to_all_tiers(true);
    if instance.config.maxDownloadRate > 0 {
        settings.SetDownload_rate_limit(instance.config.maxDownloadRate * 1024)
    }
    if instance.config.maxUploadRate > 0 {
        settings.SetUpload_rate_limit(instance.config.maxUploadRate * 1024)
    }
    instance.session.Set_settings(settings)

    log.Println("Setting Encryption settings...")
    encryptionSettings := libtorrent.NewPe_settings()
    encryptionSettings.SetOut_enc_policy(libtorrent.LibtorrentPe_settingsEnc_policy(instance.config.encryption))
    encryptionSettings.SetIn_enc_policy(libtorrent.LibtorrentPe_settingsEnc_policy(instance.config.encryption))
    encryptionSettings.SetAllowed_enc_level(libtorrent.Pe_settingsBoth)
    encryptionSettings.SetPrefer_rc4(true)
    instance.session.Set_pe_settings(encryptionSettings)
}

func startHTTP() {
    log.Println("Starting HTTP Server...")
    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/ls", lsHandler)
    http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(instance.torrentFS)))
    http.HandleFunc("/shutdown", func (w http.ResponseWriter, r *http.Request) {
        go shutdown()
        fmt.Fprintf(w, "OK")
    })

    log.Printf("Listening HTTP on %s...\n", instance.config.bindAddress)
    http.ListenAndServe(instance.config.bindAddress, nil)
}

func main() {
    // Make sure we are properly multithreaded, on a minimum of 2 threads
    // because we lock the main thread for libtorrent.
    maxProcs := runtime.NumCPU()
    if maxProcs < 2 {
        maxProcs = 2
    }
    runtime.GOMAXPROCS(maxProcs)

    // Lock the main thread.
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    parseFlags()

    log.Println("Starting BT engine...")
    instance.session = libtorrent.NewSession()
    instance.session.Listen_on(libtorrent.NewPair_int_int(6900, 6999))

    configureSession()

    startServices()

    log.Println("Parsing magnet link")
    torrentParams := libtorrent.Parse_magnet_uri2(instance.config.magnetUri)

    log.Println("Setting save path")
    torrentParams.SetSave_path(instance.config.downloadPath)

    if instance.config.noSparseFile {
        log.Println("Disabling sparse file support...")
        torrentParams.SetStorage_mode(libtorrent.Storage_mode_allocate)
    }

    log.Println("Adding torrent")
    instance.torrentHandle = instance.session.Add_torrent(torrentParams)

    log.Println("Enabling sequential download")
    instance.torrentHandle.Set_sequential_download(true)

    log.Printf("Downloading: %s\n", torrentParams.GetName())

    instance.torrentFS = NewTorrentFS(instance.torrentHandle)

    // Handle SIGTERM (Ctrl-C)
    go func() {
        signalChan := make(chan os.Signal, 1)
        signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
        <- signalChan
        go shutdown()
    }()

    // Handle self-killing when the parent dies
    go func () {
        for {
            // did the parent die? shutdown!
            if os.Getppid() == 1 {
                go shutdown()
                break
            }
            time.Sleep(1 * time.Second)
        }
    }()

    go startHTTP()

    for f := range mainFuncChan {
        f()
    }
}
