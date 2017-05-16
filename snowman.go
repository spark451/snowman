package snowman

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/spark451/snowman/snowplow"

	mgo "gopkg.in/mgo.v2"
)

// Settings defines the properites of the library for pulling the most recent
// events from the snowplow collection matching the particular query fed by
// Genquery.
type Settings struct {
	lastETL             time.Time
	SrcMgoDatabase      string
	SrcMgoCollection    string
	SrcMgoConnectionURI string
	Genquery            func(time.Time) interface{}
	Trackingfile        string
}

//MongoGet pulls latest data from DB according to query function set in Genquery
func (f *Settings) MongoGet(processRecord func(snowplow.Event)) {
	if len(f.Trackingfile) > 0 {
		lderr := f.loadposition()
		if lderr != nil {
			log.Fatal(lderr)
		}
		defer f.saveposition()
	}
	var result snowplow.Event
	session, err := mgo.Dial(f.SrcMgoConnectionURI)
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Optional. Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	c := session.DB(f.SrcMgoDatabase).C(f.SrcMgoCollection)
	iter := c.Find(f.Genquery(f.lastETL)).Iter()
	for iter.Next(&result) {
		processRecord(result)
		if f.lastETL.Before(result.ETLTimestamp) {
			f.lastETL = result.ETLTimestamp
		}
	}
	if ierr := iter.Close(); ierr != nil {
		log.Fatal(ierr)
	}

}

func (f *Settings) saveposition() {
	_ = ioutil.WriteFile(f.Trackingfile, []byte(f.lastETL.String()), 0644)
}

func (f *Settings) loadposition() error {
	dat, ferr := ioutil.ReadFile(f.Trackingfile)
	if ferr != nil {
		fmt.Println(ferr)
		return nil
	}
	layout := "2006-01-02 15:04:05.999999999 -0700 MST"
	value := string(dat)
	parsedtime, _ := time.Parse(layout, value)
	erroredtime, _ := time.Parse(layout, "0001-01-01 00:00:00 +0000 UTC")
	if parsedtime.Equal(erroredtime) {
		err := errors.New("Unrecognized date format in file: " + f.Trackingfile)
		return err
	}
	f.lastETL = parsedtime
	return nil
}
