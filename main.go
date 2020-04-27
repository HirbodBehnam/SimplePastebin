package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/rs/xid"
	bolt "go.etcd.io/bbolt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

var db *bolt.DB
var config struct{
	DatabasePath string `json:"database_path"`
	Listen string		`json:"listen"`
	Timeout int			`json:"timeout"` // The minutes before pastes timeout; Set zero to disable timeout
}

func main() {
	var err error
	// set config
	{
		cPath := "config.json"
		if len(os.Args) > 1{
			cPath = os.Args[1]
		}
		// read the json
		cBytes, err := ioutil.ReadFile(cPath)
		if err != nil{
			log.Fatalln("Cannot read config file")
		}
		err = json.Unmarshal(cBytes,&config)
		if err != nil{
			log.Fatalln("Cannot parse config file")
		}
	}
	// setup database
	db, err = bolt.Open(config.DatabasePath, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_ , innerError := tx.CreateBucketIfNotExists([]byte("pastes"))
		return innerError
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	// start cleaner; all pastes older than one day will be wiped in one hour
	go func() {
		if config.Timeout == 0{
			return
		}
		var Timeout = time.Minute * time.Duration(config.Timeout)
		for{
			time.Sleep(time.Hour)
			_ = db.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket([]byte("pastes"))
				_ = bucket.ForEach(func(k, v []byte) error {
					id , e := xid.FromBytes(k)
					if e == nil{
						if time.Since(id.Time()) > Timeout{
							_ = bucket.Delete(k)
						}
					}
					return nil
				})
				return nil
			})
		}
	}()
	// setup listener
	r := mux.NewRouter()
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		html,err := ioutil.ReadFile("index.html")
		if err != nil{
			_, _ = w.Write([]byte("Cannot read index"))
		}
		_, _ = w.Write(html)
	})
	r.HandleFunc("/submit", HandleSubmit)
	r.HandleFunc("/paste/{id}", HandleShowPaste)
	srv := &http.Server{
		Addr:         config.Listen,
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler: r,
	}
	log.Fatalln(srv.ListenAndServe())
}
func HandleShowPaste(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// parse id
	guid, err := xid.FromString(vars["id"])
	if err != nil{
		_,_ = w.Write([]byte("Cannot parse id"))
		return
	}
	// read database
	err = db.View(func(tx *bolt.Tx) error {
		paste := tx.Bucket([]byte("pastes")).Get(guid.Bytes())
		_,_ = w.Write(paste)
		return nil
	})
	if err != nil{
		_,_ = w.Write([]byte("Cannot read paste :(\n" + err.Error()))
	}
}

func HandleSubmit(w http.ResponseWriter, r *http.Request)  {
	// parse form
	err := r.ParseForm()
	if err != nil{
		_,_ = w.Write([]byte("Cannot parse form"))
		return
	}
	// get paste
	paste := r.Form.Get("paste")
	guid := xid.New()
	if paste == ""{
		_,_ = w.Write([]byte("Cannot post nothing"))
		return
	}
	if len(paste) > 51200{
		_,_ = w.Write([]byte("Paste too big"))
		return
	}
	// save it to db
	err = db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pastes")).Put(guid.Bytes(),[]byte(paste))
	})
	// redirect to page
	http.Redirect(w, r, "/paste/" + guid.String(), http.StatusSeeOther)
}