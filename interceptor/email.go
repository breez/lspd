package interceptor

import (
	"bytes"
	"encoding/hex"
	"html/template"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/breez/lspd/config"
)

const (
	charset = "UTF-8"
)

var OpenChannelEmailConfig *config.Email = &config.Email{}

func sendEmail(conf *config.Email, content, subject string) error {

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		log.Printf("Error in session.NewSession: %v", err)
		return err
	}
	svc := ses.New(sess)

	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			CcAddresses: conf.Cc,
			ToAddresses: conf.To,
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
		Source: aws.String(conf.From),
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
	channelPoint string,
	tag *string,
) error {
	var html bytes.Buffer

	tpl := `
	<table>
	<tr><td>Payment Hash:</td><td>{{ .PaymentHash }}</td></tr>
	<tr><td>Incoming Amount (msat):</td><td>{{ .IncomingAmountMsat }}</td></tr>
	<tr><td>Destination Node:</td><td>{{ .Destination }}</td></tr>
	<tr><td>Channel capacity (sat):</td><td>{{ .Capacity }}</td></tr>
	<tr><td>Channel point:</td><td>{{ .ChannelPoint }}</td></tr>
	<tr><td>Tag:</td><td>{{ .Tag }}</td></tr>
	</table>
	`
	t, err := template.New("OpenChannelEmail").Parse(tpl)
	if err != nil {
		return err
	}

	tagStr := ""
	if tag != nil {
		tagStr = *tag
	}
	if err := t.Execute(&html, map[string]string{
		"PaymentHash":        hex.EncodeToString(paymentHash),
		"IncomingAmountMsat": strconv.FormatUint(uint64(incomingAmountMsat), 10),
		"Destination":        hex.EncodeToString(destination),
		"Capacity":           strconv.FormatUint(uint64(capacity), 10),
		"ChannelPoint":       channelPoint,
		"Tag":                tagStr,
	}); err != nil {
		return err
	}

	err = sendEmail(
		OpenChannelEmailConfig,
		html.String(),
		"Open Channel - Interceptor",
	)
	if err != nil {
		log.Printf("Error sending open channel email: %v", err)
		return err
	}

	return nil
}
