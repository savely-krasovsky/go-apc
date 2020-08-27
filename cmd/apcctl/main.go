package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

	if err := client.AttachJob(jobName); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DetachJob(); err != nil {
			log.Println(err)
		}
	}()

	if err := client.SetDataField(apc.ListTypeOutbound, "DEBT_ID"); err != nil {
		panic(err)
	}
	if err := client.SetDataField(apc.ListTypeOutbound, "CURPHONE"); err != nil {
		panic(err)
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

			if event.Keyword == "AGTCallNotify" {
				for _, s := range event.Segments {
					parts := strings.Split(s, ",")
					if len(parts) == 2 {
						if parts[0] == "CURPHONE" {
							id, err := strconv.Atoi(parts[1])
							if err != nil {
								log.Println(err)
								break
							}

							field, err := client.ReadField(apc.ListTypeOutbound, "PHONE_ID"+strconv.Itoa(id))
							if err != nil {
								log.Println(err)
								break
							}
							fmt.Println(field)
						}
					}
				}
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
