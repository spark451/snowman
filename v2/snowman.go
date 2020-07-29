package snowman

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/remeh/sizedwaitgroup"
	"github.com/spark451/snowman/v2/snowplow"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Geo struct contains event long and lat data.
type Geo struct {
	Lng float32 `bson:"lng"`
	Lat float32 `bson:"lat"`
}

// MongoEvent struct contains snowplow event data
type MongoEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	snowplow.Event `bson:",inline"`
	MongoUserID    primitive.ObjectID `bson:"userid_mgo,omitempty"`
	GeoCoord       *Geo               `bson:"geo_coord,omitempty"`
}

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
	Threads             int
}

//MongoGet pulls latest data from DB according to query function set in Genquery
func (f *Settings) MongoGet(processRecord func(MongoEvent) error) {
	// Handle interupt and sigterm so that pre-mature killing of the program
	// can pick up where it left off.
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		_ = <-sigs
		done <- true
		fmt.Println("Signal received...")
		time.Sleep(5 * time.Second)
		fmt.Println("...force cleanup")
		f.saveposition()
		os.Exit(1)
	}()
	// If there is a tracking file specified, load its position and defer
	// saving its position.
	if len(f.Trackingfile) > 0 {
		lderr := f.loadposition()
		if lderr != nil {
			log.Fatal(lderr)
		}
		defer f.saveposition()
	}

	var result MongoEvent
	session, err := mongo.Connect(
		context.Background(),
		options.Client().ApplyURI(f.SrcMgoConnectionURI),
	)
	if err != nil {
		panic(err)
	}
	defer session.Disconnect(context.Background())

	// Set DB and Collection and make query.
	c := session.Database(f.SrcMgoDatabase).Collection(f.SrcMgoCollection)
	iter, err := c.Find(
		context.Background(),
		f.Genquery(f.lastETL),
		options.Find().SetSort(bson.D{{Key: "etl_timestamp", Value: 1}}),
	)
	if err != nil {
		panic(err)
	}

	// Set max thread count.
	swg := sizedwaitgroup.New(f.Threads)

	// Iterate events and call processRecord function
iterator:
	for iter.Next(context.Background()) {
		if err := iter.Decode(&result); err != nil {
			log.Fatal(err)
		}
		select {
		// Stop, an interupt signal was received or error occured in one of the threads
		case <-done:
			fmt.Println("Cleaning up...")
			break iterator
		default:
			// Increment thread counter.
			swg.Add()
			// Wrapper for function call to process record, makes threading easier.
			go procwrap(result, &swg, processRecord, done)
			// Set our lastETL counter if this record's ETL is > than others.
			if f.lastETL.Before(result.ETLTimestamp) {
				f.lastETL = result.ETLTimestamp
			}
		}
	}
	if ierr := iter.Close(context.Background()); ierr != nil {
		log.Fatal(ierr)
	}
	// Wait until all threads are complete.
	swg.Wait()

}

//Wrapper for processing the results
func procwrap(result MongoEvent, waitGroup *sizedwaitgroup.SizedWaitGroup, procfunc func(MongoEvent) error, done chan<- bool) {
	defer waitGroup.Done()
	err := procfunc(result)
	if err != nil {
		done <- true
	}
}

// Save lastETL position in specified file
func (f *Settings) saveposition() {
	if f.lastETL == *new(time.Time) {
		return
	}
	_ = ioutil.WriteFile(f.Trackingfile, []byte(f.lastETL.String()), 0644)
	fmt.Println("Saving position: ", f.lastETL.String())
}

// Load lastETL position from specified file
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
