package main

import (
	"context"
	"flag"
	"fmt"
	"log"

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

	client, err := apc.NewClient(addr, apc.WithLogger(), apc.WithNativeHTTPClient())
	if err != nil {
		panic(err)
	}

	shutdown := make(chan error)
	go func(shutdown chan<- error) {
		shutdown <- client.Start()
	}(shutdown)

	if err := client.Logon(context.Background(), agentName, password); err != nil {
		panic(err)
	}

	defer func() {
		if err := client.Logoff(context.Background()); err != nil {
			log.Println(err)
		}
	}()

	for n := range client.Notifications(context.Background()) {
		fmt.Println(n.Type)
	}

	/*if err := client.ReserveHeadset(context.Background(), headsetID); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.FreeHeadset(context.Background()); err != nil {
			log.Println(err)
		}
	}()

	if err := client.ConnectHeadset(context.Background()); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DisconnectHeadset(context.Background()); err != nil {
			log.Println(err)
		}
	}()

	if err := client.AttachJob(context.Background(), jobName); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.DetachJob(context.Background()); err != nil {
			log.Println(err)
		}
	}()

	keys, err := client.ListState(context.Background())
	if err != nil {
		panic(err)
	}
	_ = keys

	if err := client.SetDataField(context.Background(), apc.ListTypeOutbound, "DEBT_ID"); err != nil {
		panic(err)
	}
	if err := client.SetDataField(context.Background(), apc.ListTypeOutbound, "CURPHONE"); err != nil {
		panic(err)
	}

	if err := client.AvailWork(context.Background()); err != nil {
		panic(err)
	}
	defer func() {
		if err := client.NoFurtherWork(context.Background()); err != nil {
			log.Println(err)
		}
	}()

	if err := client.ReadyNextItem(context.Background()); err != nil {
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

							field, err := client.ReadField(context.Background(), apc.ListTypeOutbound, "PHONE_ID"+strconv.Itoa(id))
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
				if err := client.ReleaseLine(context.Background()); err != nil {
					log.Println(err)
				}

				if err := client.FinishedItem(context.Background(), 22); err != nil {
					log.Println(err)
				}

				if err := client.ReadyNextItem(context.Background()); err != nil {
					log.Println(err)
				}
			}
		}
	}*/
}
