package main

import (
	"github.com/Fengxq2014/mars/etcdsrv"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/smartystreets/assertions/should"
	. "github.com/smartystreets/goconvey/convey"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

var ma *mars

func TestMain(m *testing.M) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	if err != nil {
		log.Fatal(err)
	}
	ma = &mars{
		etcdCli: etcdsrv.New().Cli,
	}
	m.Run()
}

func TestHttpHandler(t *testing.T) {
	tt := []struct {
		routeVariable string
	}{
		{"/id"},
	}

	Convey("TestHttpHandler should return http Status ok, and response body contains snowflake id", t, func() {
		for _, tc := range tt {
			Convey("Test "+tc.routeVariable, func() {
				req, err := http.NewRequest("GET", tc.routeVariable, nil)
				if err != nil {
					t.Fatal(err)
				}

				rr := httptest.NewRecorder()

				router := mux.NewRouter()
				router.HandleFunc("/id", GetID).Methods("GET")
				router.ServeHTTP(rr, req)

				So(rr.Code, ShouldEqual, http.StatusOK)
				So(len(rr.Body.Bytes()), ShouldEqual, 18)
			})
		}
	})
}

func TestCampaignLeader(t *testing.T) {
	Convey(`CampaignLeader first time should return (true, "", nil), second time should return (false, "sss", TxnFailed)`, t, func() {
		Convey("Test first time", func() {
			b, s, err := ma.campaignLeader()
			So(b, ShouldEqual, true)
			So(s, ShouldBeEmpty)
			So(err, ShouldBeNil)
		})
		Convey("Test second time", func() {
			b, s, err := ma.campaignLeader()
			So(b, ShouldEqual, false)
			So(s, ShouldNotBeEmpty)
			So(err, ShouldEqual, TxnFailed)
		})
	})
}

func TestGetNode(t *testing.T) {
	Convey("GetNode should return a number", t, func() {
		Convey("Test first time", func() {
			i, err := ma.getNodeNum()
			So(err, ShouldBeNil)
			So(i, should.BeGreaterThanOrEqualTo, 1)
		})
		Convey("Test second time", func() {
			i, err := ma.getNodeNum()
			So(err, ShouldBeNil)
			So(i, should.BeGreaterThanOrEqualTo, 2)
		})
	})
}
