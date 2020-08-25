package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const (
	charset = "UTF-8"
)

func addresses(a string) (addr []*string) {
	json.Unmarshal([]byte(a), &addr)
	return
}

func sendEmail(to, cc, from, content, subject string) error {

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		log.Printf("Error in session.NewSession: %v", err)
		return err
	}
	svc := ses.New(sess)

	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			CcAddresses: addresses(cc),
			ToAddresses: addresses(to),
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Html: &ses.Content{
					Charset: aws.String(charset),
					Data:    aws.String(content),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String(charset),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String(from),
	}
	// Attempt to send the email.
	result, err := svc.SendEmail(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case ses.ErrCodeMessageRejected:
				log.Println(ses.ErrCodeMessageRejected, aerr.Error())
			case ses.ErrCodeMailFromDomainNotVerifiedException:
				log.Println(ses.ErrCodeMailFromDomainNotVerifiedException, aerr.Error())
			case ses.ErrCodeConfigurationSetDoesNotExistException:
				log.Println(ses.ErrCodeConfigurationSetDoesNotExistException, aerr.Error())
			default:
				log.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			log.Println(err.Error())
		}
		return err
	}

	log.Printf("Email sent with result:\n%v", result)

	return nil
}

func sendOpenChannelEmailNotification(
	paymentHash []byte, incomingAmountMsat int64,
	destination []byte, capacity int64,
	fundingTxID []byte, fundingTxOutnum uint32) error {

	var h chainhash.Hash
	err := h.SetBytes(fundingTxID)
	if err != nil {
		log.Printf("h.SetBytes(%x) error: %v", fundingTxID, err)
		return fmt.Errorf("h.SetBytes(%x) error: %w", fundingTxID, err)
	}
	channelPoint := wire.NewOutPoint(&h, fundingTxOutnum).String()

	var html bytes.Buffer

	tpl := `
	<table>
	<tr><td>Payment Hash:</td><td>{{ .PaymentHash }}</td></tr>
	<tr><td>Incoming Amount (msat):</td><td>{{ .IncomingAmountMsat }}</td></tr>
	<tr><td>Destination Node:</td><td>{{ .Destination }}</td></tr>
	<tr><td>Channel capacity (sat):</td><td>{{ .Capacity }}</td></tr>
	<tr><td>Channel point:</td><td>{{ .ChannelPoint }}</td></tr>
	</table>
	`
	t, err := template.New("OpenChannelEmail").Parse(tpl)
	if err != nil {
		return err
	}

	if err := t.Execute(&html, map[string]string{
		"PaymentHash":        hex.EncodeToString(paymentHash),
		"IncomingAmountMsat": strconv.FormatUint(uint64(incomingAmountMsat), 10),
		"Destination":        hex.EncodeToString(destination),
		"Capacity":           strconv.FormatUint(uint64(capacity), 10),
		"ChannelPoint":       channelPoint,
	}); err != nil {
		return err
	}

	err = sendEmail(
		os.Getenv("OPENCHANNEL_NOTIFICATION_TO"),
		os.Getenv("OPENCHANNEL_NOTIFICATION_CC"),
		os.Getenv("OPENCHANNEL_NOTIFICATION_FROM"),
		html.String(),
		"Open Channel - Interceptor",
	)
	if err != nil {
		log.Printf("Error sending open channel email: %v", err)
		return err
	}

	return nil
}
