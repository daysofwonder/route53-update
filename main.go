package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

var (
	domain     string
	zoneID     string
	ttl        int64
	ip         string
	ipMetadata bool
	ipFile     string
	waitForIt  bool
)

func findTargetByFile(ipFile string) (string, error) {
	file, err := os.Open(ipFile)
	if err != nil {
		return "", fmt.Errorf("can't open ip file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Scan()

	ip := scanner.Text()

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return strings.TrimSpace(ip), nil
}

func findTargetByMetadata(sess *session.Session) (string, error) {
	svc := ec2metadata.New(sess)

	if !svc.Available() {
		return "", fmt.Errorf("Metadata service unavailable")
	}

	ip, err := svc.GetMetadata("public-ipv4")
	if err != nil {
		return "", err
	}

	return ip, nil
}

func main() {

	flag.StringVar(&domain, "domain", "", "domain name")
	flag.StringVar(&ip, "ip", "", "target of domain name")
	flag.StringVar(&ipFile, "ip-file", "", "get IP from file")
	flag.BoolVar(&ipMetadata, "ip-metadata", false, "get IP from EC2 metadata service")
	flag.BoolVar(&waitForIt, "wait", false, "wait for DNS changes to propagate")
	flag.StringVar(&zoneID, "zone", "", "AWS Zone Id for domain")
	flag.Int64Var(&ttl, "ttl", int64(60), "TTL for DNS Cache")

	flag.Parse()

	sess, err := session.NewSession()
	if err != nil {
		fmt.Println("failed to create session,", err)
		return
	}

	target := ""

	if ip != "" {
		target = ip
	} else if ipFile != "" {
		target, err = findTargetByFile(ipFile)
	} else if ipMetadata {
		target, err = findTargetByMetadata(sess)
	}

	log.Printf("setting %s to IP %s in zone %s\n", domain, target, zoneID)

	svc := route53.New(sess)

	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{ // Required
			Changes: []*route53.Change{ // Required
				{ // Required
					Action: aws.String("UPSERT"), // Required
					ResourceRecordSet: &route53.ResourceRecordSet{ // Required
						Name: aws.String(domain), // Required
						Type: aws.String("A"),    // Required
						ResourceRecords: []*route53.ResourceRecord{
							{ // Required
								Value: aws.String(target), // Required
							},
						},
						TTL:           aws.Int64(ttl),
						Weight:        aws.Int64(int64(1)),
						SetIdentifier: aws.String("Arbitrary Id describing this change set"),
					},
				},
			},
			Comment: aws.String("Sample update."),
		},
		HostedZoneId: aws.String(zoneID), // Required
	}
	resp, err := svc.ChangeResourceRecordSets(params)

	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// Get error details
			log.Println("Error:", awsErr.Code(), awsErr.Message())

			// Prints out full error message, including original error if there was one.
			log.Println("Error:", awsErr.Error())

			// Get original error
			if origErr := awsErr.OrigErr(); origErr != nil {
				// operate on original error.
				fmt.Println(origErr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}
		return
	}

	if waitForIt {
		log.Printf("waiting changes to propagate: %s\n", *resp.ChangeInfo.Comment)

		changeInput := &route53.GetChangeInput{
			Id: resp.ChangeInfo.Id,
		}
		err = svc.WaitUntilResourceRecordSetsChanged(changeInput)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				// Get error details
				log.Println("Error:", awsErr.Code(), awsErr.Message())

				// Prints out full error message, including original error if there was one.
				log.Println("Error:", awsErr.Error())

				// Get original error
				if origErr := awsErr.OrigErr(); origErr != nil {
					// operate on original error.
					fmt.Println(origErr.Error())
				}
			} else {
				fmt.Println(err.Error())
			}
			return
		}
		log.Println("change applied")
	} else {
		log.Println("change sent to route53")
	}
}
