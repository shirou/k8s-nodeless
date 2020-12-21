package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/lambda"
	lru "github.com/hashicorp/golang-lru"
	"go.uber.org/zap"
)

const (
	maxEventsBuffer = 10000
	maxEventsCache  = 100000
	watchSleepTime  = 500 // interval in millsec
)

// AWSServerless is a Serverless struct for AWS
type AWSServerless struct {
	funcName string
	payload  string

	awsOpts      session.Options
	startTime    time.Time
	region       string
	logGroupName string
	logClient    *cloudwatchlogs.CloudWatchLogs
	eventCache   *lru.Cache
	requestId    string
}

// NewAWSServerless returns new Serverless struct for AWS Lambda
func NewAWSServerless(config *Config) (*AWSServerless, error) {

	logGroupName, region, err := parseAWSFuncName(config.funcName)
	if err != nil {
		return nil, fmt.Errorf("parseAWSFuncName: %w", err)
	}

	awsConfig := aws.NewConfig()
	if region != "" {
		awsConfig = awsConfig.WithRegion(region)
	}
	awsOpts := session.Options{
		SharedConfigState: session.SharedConfigEnable,
		AssumeRoleTokenProvider: func() (string, error) {
			return stscreds.StdinTokenProvider()
		},
		Config: *awsConfig,
	}

	cache, err := lru.New(maxEventsCache)
	if err != nil {
		return nil, err
	}

	ret := &AWSServerless{
		funcName:     config.funcName,
		payload:      config.payload,
		startTime:    time.Now(),
		region:       region,
		logGroupName: logGroupName,
		awsOpts:      awsOpts,
		eventCache:   cache,
	}

	return ret, nil
}

// parseAWSFuncName parse funcname and get CloudWatch logs group name and region
// function name could be these format.
//    * Function name - my-function.
//    * Function ARN - arn:aws:lambda:us-west-2:123456789012:function:my-function.
//    * Partial ARN - 123456789012:function:my-function.
func parseAWSFuncName(funcName string) (string, string, error) {
	p := strings.Split(funcName, ":")
	// func name is Function name
	if len(p) == 1 {
		return fmt.Sprintf("/aws/lambda/%s", funcName), "", nil
	}
	// func name is Function ARN
	if p[0] == "arn" && p[1] == "aws" {
		return fmt.Sprintf("/aws/lambda/%s", p[6]), p[3], nil
	}
	// func name is Partial ARN
	if _, err := strconv.Atoi(p[0]); err == nil && p[1] == "function" {
		return fmt.Sprintf("/aws/lambda/%s", p[2]), "", nil
	}

	return "", "", fmt.Errorf("wrong format function name, %s", funcName)
}

// Invoke invoke AWS Lambda function
func (sl *AWSServerless) Invoke(ctx context.Context) error {
	sess, err := session.NewSessionWithOptions(sl.awsOpts)
	if err != nil {
		return fmt.Errorf("aws session error, %s: %w", sl.funcName, err)
	}
	svc := lambda.New(sess)
	input := &lambda.InvokeInput{
		FunctionName:   aws.String(sl.funcName),
		Payload:        []byte(sl.payload),
		LogType:        aws.String("Tail"),
		InvocationType: aws.String("Event"), // always async invocation
	}

	resp, err := svc.InvokeWithContext(ctx, input)

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			return fmt.Errorf("aws error, %s: %w", sl.funcName, aerr)
		}
		return fmt.Errorf("lambda invokation, %s: %w", sl.funcName, err)
	}

	if resp.FunctionError != nil {
		return fmt.Errorf("invoke lambda response error, %v: %s", string(resp.Payload), aws.StringValue(resp.FunctionError))
	}

	return sl.logTailStart(ctx)
}

func (sl *AWSServerless) logTailStart(ctx context.Context) error {
	sess, err := session.NewSessionWithOptions(sl.awsOpts)
	if err != nil {
		logger.Error("aws session error, %s: %w", sl.funcName, err)
	}

	sl.logClient = cloudwatchlogs.New(sess)

	return sl.logTail(ctx, sl.logGroupName)
}

var startRequestRe = regexp.MustCompile("START RequestId: (.+) Version:")
var endRequestRe = regexp.MustCompile("END RequestId: (.+)")

func (sl *AWSServerless) logTail(ctx context.Context, logGroupName string) error {
	lastSeenTime := aws.Int64(aws.TimeUnixMilli(sl.startTime))
	start := make(chan struct{}, 1)
	done := make(chan struct{}, 1)
	go func() {
		apiTicker := time.NewTicker(watchSleepTime * time.Millisecond)
		for range apiTicker.C {
			start <- struct{}{}
		}
	}()

	fn := func(res *cloudwatchlogs.FilterLogEventsOutput, lastPage bool) bool {
		for _, event := range res.Events {
			if _, ok := sl.eventCache.Peek(event.EventId); !ok {
				sl.eventCache.Add(event.EventId, nil)

				logger.Infow(*event.Message, zap.String("function_name", sl.funcName), zap.String("request_id", sl.requestId))

				if sl.requestId == "" {
					start := startRequestRe.FindStringSubmatch(*event.Message)
					if len(start) == 2 {
						sl.requestId = start[1]
					}
				} else {
					end := endRequestRe.FindStringSubmatch(*event.Message)
					if len(end) == 2 {
						done <- struct{}{}
						if sl.requestId == end[1] {
							logger.Infof("%s has been finished", sl.requestId)
						} else {
							logger.Infof("%s has already finished but not catched", sl.requestId)
						}
					}
				}

			}
		}
		if lastPage && len(res.Events) > 0 {
			lastSeenTime = res.Events[len(res.Events)-1].IngestionTime
		}
		return true
	}

	for {
		select {
		case <-start:
			streams, err := sl.listLogStreams(ctx, logGroupName, *lastSeenTime)
			if err != nil {
				return fmt.Errorf("listLogStreams, %s: %w", logGroupName, err)
			}
			if len(streams) == 0 {
				continue
			}
			input := &cloudwatchlogs.FilterLogEventsInput{
				StartTime:      lastSeenTime,
				LogStreamNames: streams,
				LogGroupName:   aws.String(logGroupName),
			}

			if err := sl.logClient.FilterLogEventsPages(input, fn); err != nil {
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == "ThrottlingException" {
						logger.Info("Rate exceeded for %s. Wait for 500ms then retry.\n", logGroupName)
						time.Sleep(500 * time.Millisecond)
						continue
					}
				}
				return fmt.Errorf("FilterLogEventsPages, %s: %w", logGroupName, err)
			}
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (sl *AWSServerless) listLogStreams(ctx context.Context, logGroupName string, since int64) ([]*string, error) {
	streams := make([]*string, 0, 10)
	fn := func(res *cloudwatchlogs.DescribeLogStreamsOutput, lastPage bool) bool {
		hasUpdatedStream := false
		for _, stream := range res.LogStreams {
			if stream.FirstEventTimestamp == nil || stream.LastEventTimestamp == nil || stream.LastIngestionTime == nil || stream.UploadSequenceToken == nil {
				continue
			}

			// Use LastIngestionTime because LastEventTimestamp is updated slowly...
			if *stream.LastIngestionTime < since {
				continue
			}
			hasUpdatedStream = true
			streams = append(streams, stream.LogStreamName)
		}
		return hasUpdatedStream
	}

	input := &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(logGroupName),
		OrderBy:      aws.String("LastEventTime"),
		Descending:   aws.Bool(true),
	}

	if err := sl.logClient.DescribeLogStreamsPagesWithContext(ctx, input, fn); err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == "ResourceNotFoundException" {
				return streams, nil
			} else if awsErr.Code() == "ThrottlingException" {
				time.Sleep(500 * time.Millisecond)
				return nil, nil
			}
		}
		return nil, fmt.Errorf("DescribeLogStreams, %w", err)
	}
	return streams, nil
}
