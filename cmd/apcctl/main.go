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

	client, err := apc.NewClient(addr, apc.WithLogger())
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

	if err := client.AttachJob(jobName); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DetachJob(); err != nil {
			log.Println(err)
		}
	}()

	fields := []string{
		"DEBT_ID",
		"PHONE1",
		"PHONE2",
		"PHONE3",
		"PHONE4",
		"PHONE5",
		"PHONE6",
		"PHONE7",
		"PHONE8",
		"PHONE9",
		"PHONE10",
		"PHONE_ID1",
		"PHONE_ID2",
		"PHONE_ID3",
		"PHONE_ID4",
		"PHONE_ID5",
		"PHONE_ID6",
		"PHONE_ID7",
		"PHONE_ID8",
		"PHONE_ID9",
		"PHONE_ID10",
	}

	errChan := make(chan error)

	for _, fieldName := range fields {
		fieldName := fieldName
		go func() {
			errChan <- client.SetDataField(apc.ListTypeOutbound, fieldName)
		}()
	}

	counter := 0
	for err := range errChan {
		counter++

		if err != nil {
			panic(err)
		}

		if counter == len(fields) {
			break
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
	}

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
