package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gitlab.sovcombank.group/scb-mobile/lib/go-apc.git"
)

func main() {
	var (
		addr      string
		agentName string
		password  string
		headsetID int
		jobName   string
	)
	flag.StringVar(&addr, "addr", "", "Avaya Proactive Contact server address")
	flag.StringVar(&agentName, "agent-name", "", "Agent name")
	flag.StringVar(&password, "password", "", "Agent password")
	flag.IntVar(&headsetID, "headset-id", 0, "Headset ID")
	flag.StringVar(&jobName, "job-name", "", "Job name")
	flag.Parse()

	client, err := apc.NewClient(addr, apc.WithProductionLogger())
	if err != nil {
		panic(err)
	}

	shutdown := make(chan error)
	go func(shutdown chan<- error) {
		shutdown <- client.Start()
	}(shutdown)

	if err := client.Logon(agentName, password); err != nil {
		panic(err)
	}

	defer func() {
		if err := client.Logoff(); err != nil {
			log.Println(err)
		}
	}()

	if err := client.ReserveHeadset(headsetID); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.FreeHeadset(); err != nil {
			log.Println(err)
		}
	}()

	if err := client.ConnectHeadset(); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DisconnectHeadset(); err != nil {
			log.Println(err)
		}
	}()

	client.ListCallLists()

	/*if err := client.AttachJob(jobName); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DetachJob(); err != nil {
			log.Println(err)
		}
	}()

	for _, fieldName := range []string{
		"COUNTER1",
		"DEBT_ID",
		"FIO",
		"PORTFOLIO",
		"TSUMPAY",
		"CURRENCY",
		"PHONE1",
		"PHONE_ID1",
		"PHONE2",
		"PHONE_ID2",
		"PHONE3",
		"PHONE_ID3",
		"PHONE4",
		"PHONE_ID4",
		"PHONE5",
		"PHONE_ID5",
		"PHONE6",
		"PHONE_ID6",
		"PHONE7",
		"PHONE_ID7",
		"PHONE8",
		"PHONE_ID9",
		"PHONE10",
		"PHONE_ID10",
	} {
		if err := client.SetDataField(apc.ListTypeOutbound, fieldName); err != nil {
			log.Println(err)
		}
	}

	if err := client.AvailWork(); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.NoFurtherWork(); err != nil {
			log.Println(err)
		}
	}()

	if err := client.ReadyNextItem(); err != nil {
		log.Println(err)
	}*/

	// Graceful shutdown block
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			return
		case <-shutdown:
			return
		case event, ok := <-client.Notifications():
			if !ok {
				fmt.Println("notification channel closed!")
				return
			}

			if event.Keyword == "AGTAutoReleaseLine" {
				if err := client.ReleaseLine(); err != nil {
					log.Println(err)
				}

				if err := client.FinishedItem(35); err != nil {
					log.Println(err)
				}

				if err := client.ReadyNextItem(); err != nil {
					log.Println(err)
				}
			}
		}
	}
}
