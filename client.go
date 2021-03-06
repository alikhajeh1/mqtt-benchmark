package main

import (
	"fmt"
	"log"
	"time"
)

import (
	stats "github.com/GaryBoone/GoStats/stats"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	ID         int
	BrokerURL  string
	BrokerUser string
	BrokerPass string
	MsgTopic   string
	MsgSize    int
	MsgCount   int
	MsgTimeOut   int
	MsgDelay   int
	MsgQoS     byte
	Quiet      bool
}

func (c *Client) Run(res chan *RunResults) {
	newMsgs := make(chan *Message)
	pubMsgs := make(chan *Message)
	doneGen := make(chan bool)
	donePub := make(chan bool)
	runResults := new(RunResults)

	started := time.Now()
	// start generator
	go c.genMessages(newMsgs, doneGen)
	// start publisher
	go c.pubMessages(newMsgs, pubMsgs, doneGen, donePub)

	runResults.ID = c.ID
	runResults.Total = int64(c.MsgCount)
	times := []float64{}
	for {
		select {
		case m := <-pubMsgs:
			if m.Error {
				// log.Printf("CLIENT %v ERROR publishing message: %v: at %v\n", c.ID, m.Topic, m.Sent.Unix())
				runResults.Failures++
			} else {
				// log.Printf("Message published: %v: sent: %v delivered: %v flight time: %v\n", m.Topic, m.Sent, m.Delivered, m.Delivered.Sub(m.Sent))
				runResults.Successes++
				times = append(times, m.Delivered.Sub(m.Sent).Seconds()*1000) // in milliseconds
			}
		case <-donePub:
			// calculate results
			duration := time.Now().Sub(started)
			runResults.MsgTimeMin = stats.StatsMin(times)
			runResults.MsgTimeMax = stats.StatsMax(times)
			runResults.MsgTimeMean = stats.StatsMean(times)
			runResults.MsgTimeStd = stats.StatsSampleStandardDeviation(times)
			runResults.RunTime = duration.Seconds()
			runResults.MsgsPerSec = float64(runResults.Successes) / duration.Seconds()

			// report results and exit
			res <- runResults
			return
		}
	}
}

func (c *Client) genMessages(ch chan *Message, done chan bool) {
	for i := 0; i < c.MsgCount; i++ {
		ch <- &Message{
			Topic:   c.MsgTopic,
			QoS:     c.MsgQoS,
			Payload: make([]byte, c.MsgSize),
		}
		// delay betweeen each message send
		time.Sleep(time.Duration(c.MsgDelay)*time.Millisecond)
	}
	done <- true
	// log.Printf("CLIENT %v is done generating messages\n", c.ID)
	return
}

func (c *Client) pubMessages(in, out chan *Message, doneGen, donePub chan bool) {
	onConnected := func(client mqtt.Client) {
		if !c.Quiet {
			log.Printf("CLIENT %v is connected to the broker %v\n", c.ID, c.BrokerURL)
		}
		ctr := 0
		for {
			select {
			case m := <-in:
				m.Sent = time.Now()
				token := client.Publish(m.Topic, m.QoS, false, m.Payload)
				// token.Wait()
				send_timeout := false
				if token.WaitTimeout(time.Duration(c.MsgTimeOut) * time.Millisecond) == false { //timed out
					send_timeout = true
				}
				if token.Error() != nil {
					log.Printf("CLIENT %v Error sending message: %v\n", c.ID, token.Error())
					m.Error = true
				} else {
					if send_timeout == true{
						log.Printf("CLIENT %v TIMEOUT while sending message: %v\n", c.ID, token.Error())
						m.Error = true
					} else {
						m.Delivered = time.Now()
						m.Error = false
					}
				}
				out <- m

				if ctr > 0 && ctr%100 == 0 {
					if !c.Quiet {
						log.Printf("CLIENT %v published %v messages and keeps publishing...\n", c.ID, ctr)
					}
				}
				ctr++
				if ctr >= c.MsgCount{
					if client.IsConnected() {
						log.Printf("CLIENT %v disconected after published %v messages\n", c.ID, ctr)
						client.Disconnect(100);
					}
					donePub <- true
					//if !c.Quiet {
					//	log.Printf("CLIENT %v is done publishing\n", c.ID)
					//}
					return;
				}
			case <-doneGen:
				//donePub <- true
				if !c.Quiet {
					log.Printf("CLIENT %v is done generating msg\n", c.ID)
				}
				//if client.IsConnected() {
				//	log.Printf("CLIENT %v disconected due to doneGen\n", c.ID)
				//	client.Disconnect(0);
				//}
				//return
			}
		}
	}

	opts := mqtt.NewClientOptions().
		AddBroker(c.BrokerURL).
		SetClientID(fmt.Sprintf("mqtt-benchmark-%v-%v", time.Now(), c.ID)).
		SetCleanSession(true).
		SetConnectTimeout(3* time.Second).
		SetAutoReconnect(true).
		SetOnConnectHandler(onConnected).
		SetConnectionLostHandler(func(client mqtt.Client, reason error) {
			log.Printf("CLIENT %v lost connection to the broker: %v. but will reconnect.\n", c.ID, reason.Error())
		})
	if c.BrokerUser != "" && c.BrokerPass != "" {
		opts.SetUsername(c.BrokerUser)
		opts.SetPassword(c.BrokerPass)
	}
	client := mqtt.NewClient(opts)
	token := client.Connect()
	//token.Wait()
	// maximum wait for error
	timeout := false
	if token.WaitTimeout(10 * time.Second) == false { //timed out
		timeout = true
	}

	if token.Error() != nil {
		donePub <- true
		log.Printf("CLIENT %v had error connecting to the broker: %v\n", c.ID, token.Error())
	}else if timeout == true{
		donePub <- true
		log.Printf("CLIENT %v had time out while connecting to the broker: %v\n", c.ID, token.Error())
	}

}
