package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/web"
)

/////////////////////////////////////////
// app const & global variables

var serverUrl *string
var cptIterations *int
var monkeyTestLevel *int
var concurrency *int
var deleteAllJobsBeforeStart *bool
var useHTTP1_1 *bool
var jobsStatus sync.Map   // thread-safe map
var jobsFailures sync.Map // count the number of failures for each job

const JobMaxFailures = 3 // this number must be the same as the server yaml configuration retryPolicy: maxRetry

/////////////////////////////////////////
// consumer const & global variables

// List of potential actions to perform on a job
const (
	ActionSuccess = 0
	ActionCancel  = 1
	ActionFailure = 2
	ActionTimeout = 3
)

// Define action weights for each monkeyLevel
// Use monkeyTestLevel to simulate various behaviors such as:
// - level 0: simulate job success (default behavior)
// - level 1: also simulate job cancel
// - level 2: also simulate job faillure
// - level 3: also simulate job timeout (no response)
// - level 4: also simulate job change visibility timeout
// - level 5: also simulate server restart then simulate job success
var actionWeights = map[int]map[int]int{
	0: {
		ActionSuccess: 100,
		ActionCancel:  0,
		ActionFailure: 0,
		ActionTimeout: 0,
	},
	1: {
		ActionSuccess: 80,
		ActionCancel:  20,
		ActionFailure: 0,
		ActionTimeout: 0,
	},
	2: {
		ActionSuccess: 50,
		ActionCancel:  25,
		ActionFailure: 25,
		ActionTimeout: 0,
	},
	3: {
		ActionSuccess: 45,
		ActionCancel:  10,
		ActionFailure: 19,
		ActionTimeout: 1,
	},
	4: {
		ActionSuccess: 14,
		ActionCancel:  30,
		ActionFailure: 30,
		ActionTimeout: 1,
	},
}

/////////////////////////////////////////
// producer const & global variables

const TEST_TOPIC_1 = "service.load_test.1"
const TEST_TOPIC_2 = "service.load_test.2"
const ALL_TOPICS = "*"

// List of job definitions
const (
	JobDefWithTopic1          = 0
	JobDefWithTopic2          = 1
	JobDefWithLockedResources = 2
	JobDefWithDelayedTopic1   = 3
)

// Define action weights for each monkeyLevel
// Use monkeyTestLevel to generate various job scenarios such as:
var jobScenarioDefinitionWeights = map[int]map[int]int{
	0: {
		JobDefWithTopic1:          100,
		JobDefWithTopic2:          0,
		JobDefWithLockedResources: 0,
	},
	1: {
		JobDefWithTopic1:          50,
		JobDefWithTopic2:          50,
		JobDefWithLockedResources: 0,
	},
	2: {
		JobDefWithTopic1:          50,
		JobDefWithTopic2:          48,
		JobDefWithLockedResources: 2,
	},
	3: {
		JobDefWithTopic1:          40,
		JobDefWithTopic2:          48,
		JobDefWithLockedResources: 2,
		JobDefWithDelayedTopic1:   10,
	},
}

/////////////////////////////////////////
// common functions

func getRandomValueFromWeightMap(level int, weightsMap map[int]map[int]int, defaultValue int) int {
	// depending on the level of monkeytest, get a random action
	weights, exists := weightsMap[level]
	if !exists {
		weights = weightsMap[0] // default to level 0 if level not found
	}

	totalWeight := 0
	for _, weight := range weights {
		totalWeight += weight
	}

	randomValue := rand.Intn(totalWeight)
	for action, weight := range weights {
		if randomValue < weight {
			return action
		}
		randomValue -= weight
	}

	return defaultValue
}

/////////////////////////////////////////
// producer functions

func DeleteAllJobs(client *http.Client) error {
	url := *serverUrl + "/api/v1/jobs/"
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return err
	}

	req.Header.Add("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		fmt.Printf("Failed to delete jobs: %s\n", body)
		return err
	}

	fmt.Println("All jobs deleted successfully.")
	return nil
}

func jobWithTopic(scenario int, topic string) string {
	return fmt.Sprintf(`{
		"topic": "%s",
		"properties": {
				"scenario": %d
		},
		"userAgent": "loadtest",
		"requester": "goloadtest"
	}`, topic, scenario)
}

func jobWithDelay(scenario int, topic string, delaySeconds int) string {
	return fmt.Sprintf(`{
		"topic": "%s",
		"properties": {
				"scenario": %d
		},
		"userAgent": "loadtest",
		"requester": "goloadtest",
		"delay": %d
	}`, topic, scenario, delaySeconds)
}

func jobWithLockedResources(monkeyTestLevel int) string {
	topic := TEST_TOPIC_1
	if monkeyTestLevel > 2 {
		topic = fmt.Sprintf(`%s.%d`, TEST_TOPIC_2, rand.Intn(4))
	}

	var lockResources string
	switch rand.Intn(10) {
	case 0:
		lockResources = `"resource1"`
	case 1:
		lockResources = `"resource2"`
	case 2:
		lockResources = `"resource3"`
	case 3:
		lockResources = `"resource3:abc:def"`
	case 4:
		lockResources = `"resource1","resource2"`
	case 5:
		lockResources = `"resource1","resource3"`
	case 6:
		lockResources = `"resource2","resource3"`
	case 7:
		lockResources = `"resource1","resource2","resource3"`
	case 8:
		lockResources = `"resource1","resource2","resource3:abc:def"`
	default:
		lockResources = `"resource1"`
	}

	return fmt.Sprintf(`{
		"topic": "%s",
		"properties": {
			"key1": "value1",
			"key2": 42,
			"key3": [
				1,
				2,
				3
			],
			"key4": {
				"a": 1,
				"b": [],
				"c": {
					"x": "value1",
					"y": 42
				}
			}
		},
		"lockResources": [%s],
		"userAgent": "loadtest",
		"requester": "goloadtest",
		"sessionId": "0190e951-c29a-70aa-ba59-18be4abe97a1",
		"traceId": "0190e951-c29a-70aa-ba59-18be4abe97a0"
	}`, topic, lockResources)
}

func getRandomJobScenario(level int) int {
	// depending on the level of monkeytest, get a random job scenario
	return getRandomValueFromWeightMap(level, jobScenarioDefinitionWeights, JobDefWithTopic1)
}

func jobGenerator() string {
	// choose an random job scenario depending on the level of monkeytest
	jobScenario := getRandomJobScenario(*monkeyTestLevel)
	switch jobScenario {
	case JobDefWithLockedResources:
		return jobWithLockedResources(*monkeyTestLevel)
	case JobDefWithTopic1:
		return jobWithTopic(JobDefWithTopic1, TEST_TOPIC_1)
	case JobDefWithTopic2:
		return jobWithTopic(JobDefWithTopic2, TEST_TOPIC_2)
	case JobDefWithDelayedTopic1:
		return jobWithDelay(JobDefWithTopic2, TEST_TOPIC_1, 2)
	default:
		panic("invalid job scenario")
	}
}

func createNewJob(client *http.Client, jobDesc string) (jobUUID string, err error) {
	// Create a new job
	payload := strings.NewReader(jobDesc)

	// make an HTTP request to the server to create a job
	url := *serverUrl + "/api/v1/job/"
	method := "POST"

	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		return "", err
	}

	if *useHTTP1_1 {
		// Set the HTTP protocol version to 1.1
		req.Proto = "HTTP/1.1"
		//req.Close = true // TODO: close the connection (remove this line)
	} else {
		// Set the HTTP protocol version to 1.0
		req.Proto = "HTTP/1.0"
	}

	req.Header.Add("Content-Type", "application/json")

	retryCount := 100 // max number of retries (backoff strategy for no connection)
	for i := 0; i < retryCount; i++ {
		res, err := client.Do(req)
		if err != nil {
			// if err reason is "connection refused", retry (error(syscall.Errno) 10061)
			var serr syscall.Errno
			if errors.As(err, &serr) /*&& serr == syscall.ECONNREFUSED*/ {
				// usually the error is like:
				// dial tcp 127.0.0.1:8080: connectex: Only one usage of each socket address (protocol/network address/port) is normally permitted.
				_, file, line, _ := runtime.Caller(0)
				fmt.Printf("Error at %s:%d: %s\n", file, line, err.Error())
				if res != nil {
					print("close response\n")
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
				// print("Connection refused\n")
				// time.Sleep(10 * time.Millisecond)
				// close the connection
				client.CloseIdleConnections()
				continue
			}
			// if err reason is "no connection", retry
			var netErr net.Error
			if errors.As(err, &netErr) /*&& netErr.Timeout()*/ {
				// sleep 100ms before retry
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// print the type of error
			fmt.Printf("Error type: %T\n", err)

			// other errors
			fmt.Println(err)
			return "", err
		}

		if res.StatusCode >= 300 {
			// print the body of the response
			body, err := io.ReadAll(res.Body)
			if err == nil {
				fmt.Println(string(body))
			}
			// io.Copy(ioutil.Discard, res.Body)
			res.Body.Close()
			return "", fmt.Errorf("HTTP response code: %d", res.StatusCode)
		}

		// discard the body of the response to free the connection
		// io.Copy(io.Discard, res.Body)

		// get the job UUID
		body, err := io.ReadAll(res.Body)

		// close the connection to the server to free the connection
		res.Body.Close()

		if err != nil {
			return "", err
		}

		// unmarshal the job
		var result web.JSONResultCreateJob
		err = json.Unmarshal(body, &result)
		if err != nil {
			return "", err
		}

		// get the job UUID
		jobUUID := result.Job.JobUUID.String()

		// job created successfully
		jobsStatus.Store(jobUUID, "created")
		jobsFailures.Store(jobUUID, 0)

		return jobUUID, nil
	}

	return "", fmt.Errorf("max number of retries reached")
}

func setJobVisibilityTimeout(client *http.Client, jobUUID string, timeoutSec int) error {
	// set the job's visibility timeout
	payload := strings.NewReader("")

	// make an HTTP request to the server to create a job
	url := fmt.Sprintf(
		"%s/api/v1/job/%s/visibilitytimeout?%s=%d",
		*serverUrl, jobUUID, constants.VisibilityTimeoutQueryParam, timeoutSec,
	)
	method := "POST"

	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}

	if *useHTTP1_1 {
		// Set the HTTP protocol version to 1.1
		req.Proto = "HTTP/1.1"
		//req.Close = true
	} else {
		// Set the HTTP protocol version to 1.0
		req.Proto = "HTTP/1.0"
	}

	req.Header.Add("Content-Type", "application/json")
	res, err := client.Do(req)
	// don't care about eventual errors
	if res != nil {
		res.Body.Close()
	}

	return err
}

func jobProducer(client *http.Client, jobDesc string) error {
	jobUUID, err := createNewJob(client, jobDesc)
	if err != nil {
		return err
	}

	if *monkeyTestLevel >= 4 {
		// 10% chance to change the job's visibility timeout
		if rand.Intn(10) == 0 {
			// change the job's visibility timeout
			setJobVisibilityTimeout(client, jobUUID, 2+rand.Intn(5))
		}
	}

	return nil
}

func produce(client *http.Client, func_job_producer func(*http.Client, string) error, func_job_generator func() string, tasks <-chan struct{}, completed *atomic.Uint64, cptErrors *atomic.Uint64, wg *sync.WaitGroup) {
	// this is a producer goroutine
	defer wg.Done()

	for range tasks {
		err := func_job_producer(client, func_job_generator())
		if err != nil {
			cptErrors.Add(1)
			fmt.Println(err)
		}
		completed.Add(1)
	}
}

/////////////////////////////////////////
// consumer functions

func getRandomAction(level int) int {
	// depending on the level of monkeytest, get a random action
	return getRandomValueFromWeightMap(level, actionWeights, ActionSuccess)
}

func incJobFailuesCount(jobUUID string) (jobFailuresCount int) {
	// increment the number of failures for this job
	if value, ok := jobsFailures.Load(jobUUID); ok {
		jobFailuresCount := value.(int) + 1
		jobsFailures.Store(jobUUID, jobFailuresCount)
		return jobFailuresCount
	}

	jobsFailures.Store(jobUUID, 1)
	return 1
}

func pull_job_from_queue(client *http.Client, topic string, monkeyTestLevel int) (jobPayload string, success bool, err error) {
	// Create the URL for pulling a job from the queue

	// job topic
	queryTopic := ""
	if topic != "" {
		// url encode the topic
		queryTopic = fmt.Sprintf("&%s=%s", constants.JobTopicParam, url.QueryEscape(topic))
	}

	// number of jobs to pull
	numJobs := 1
	queryNumJobs := ""
	if numJobs > 1 {
		queryNumJobs = fmt.Sprintf("&%s=%d", constants.NumJobsQueryParam, numJobs)
	}

	visibilityTimeout := -1
	if monkeyTestLevel >= 3 && rand.Intn(2) == 0 { // 50% chance to use forced visibility timeout
		// change the visibility timeout
		visibilityTimeout = 3 + rand.Intn(5)
	}
	queryVisibilityTimeout := ""
	if visibilityTimeout > 0 {
		queryVisibilityTimeout = fmt.Sprintf("&%s=%d", constants.VisibilityTimeoutQueryParam, visibilityTimeout)
	}

	// long polling
	queryWaitTimeSeconds := ""
	if monkeyTestLevel >= 4 && rand.Intn(2) == 0 { // 50% chance to use long polling
		// wait time in seconds
		waitTimeSeconds := 2 + rand.Intn(5)
		queryWaitTimeSeconds = fmt.Sprintf("&%s=%d", constants.WaitTimeSecondsQueryParam, waitTimeSeconds)
	}

	// merge the query parameters
	queryParameters := fmt.Sprintf("%s%s%s%s", queryTopic, queryNumJobs, queryVisibilityTimeout, queryWaitTimeSeconds)
	if len(queryParameters) > 0 {
		// remove the first character '&'
		queryParameters = queryParameters[1:]
	}

	url := fmt.Sprintf("%s/api/v1/job/pull?%s", *serverUrl, queryParameters)

	// Create the payload with the topic
	payload := bytes.NewBufferString(``)

	// Make an HTTP request to the server to pull a job
	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		return "", false, err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}

	// Check if the response status is not OK
	if resp.StatusCode == http.StatusNotFound {
		// no more jobs to consume (queue is empty)
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("failed to pull job: %s", body)
	}

	// Return the job as a string
	return string(body), true, nil
}

func SetJobAsSucceed(client *http.Client, jobUUID string) error {
	_, err := SetJobStatus(client, jobUUID, "/succeed")
	if err == nil {
		jobsStatus.Store(jobUUID, "success")
	}
	return err
}

func SetJobAsCancel(client *http.Client, jobUUID string) error {
	_, err := SetJobStatus(client, jobUUID, "/cancel")
	if err == nil {
		jobsStatus.Store(jobUUID, "canceled")
	}
	return err
}

func SetJobAsFail(client *http.Client, jobUUID string) (jobMaxRetryFailuresReached bool, err error) {
	bodyResponse, err := SetJobStatus(client, jobUUID, "/fail")
	if err != nil {
		return false, err
	}

	// check if the job reached max retry
	// cast resp as JSONResult
	var result web.JSONResult
	err = json.Unmarshal(*bodyResponse, &result)
	if err != nil {
		return false, err
	}

	// increment the number of failures for this job
	incJobFailuesCount(jobUUID)

	// check if the job reached max retry
	jobMaxRetryFailuresReached = result.Message == "job reached max retry"
	if jobMaxRetryFailuresReached {
		jobsStatus.Store(jobUUID, "failMaxRetry")
		return true, nil
	}

	jobsStatus.Store(jobUUID, "fail")
	return false, err
}

func SetJobStatus(client *http.Client, jobUUID string, urlSuffix string) (bodyResponse *[]byte, err error) {
	// Create the URL
	url := *serverUrl + "/api/v1/job/" + jobUUID + urlSuffix

	// Create the payload with the topic
	payload := bytes.NewBufferString(``)

	// Make an HTTP request to the server to pull a job
	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		return nil, err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	bodyResponse = &response

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		return bodyResponse, fmt.Errorf("failed to set job status: %s", bodyResponse)
	}

	return bodyResponse, nil
}

func jobConsumer(client *http.Client, topic string) (jobFound bool, jobCompleted bool, err error) {
	// choose an random action to do on the job
	action := getRandomAction(*monkeyTestLevel)

	payload, success, err := pull_job_from_queue(client, topic, *monkeyTestLevel)
	if err != nil {
		return false, false, err
	}
	if !success {
		// no more jobs to consume (queue is empty)
		return false, false, nil
	}

	// unmarshal the job
	var result web.JSONResultPullJob
	err = json.Unmarshal([]byte(payload), &result)
	if err != nil {
		return false, false, err
	}

	if len(result.Jobs) == 0 {
		return false, false, fmt.Errorf("empty result")
	}

	j := result.Jobs[0]

	jobUUID := j.JobUUID.String()

	// check if the job is already in the jobsStatus map and succeed
	jobStatus, ok := jobsStatus.Load(jobUUID)
	if ok {
		if jobStatus == "success" {
			// the job is already succeed
			// this situation can happen if the job is pulled from another consumer (aynschronous goroutine)
			return true, false, fmt.Errorf("job already succeed")
		}
	}

	switch action {
	case ActionCancel:
		// set the job as canceled
		err = SetJobAsCancel(client, jobUUID)
		if err != nil {
			return true, false, err
		}
		// successfully canceled the job
		// (don't mark job as completed because it will be started again later)
		return true, false, nil
	case ActionFailure:
		// set the job as failed
		jobMaxRetryFailuresReached, err := SetJobAsFail(client, jobUUID)
		if err != nil {
			return true, false, err
		}
		// successfully fail the job
		if jobMaxRetryFailuresReached {
			// mark this job as completed
			return true, true, nil
		} else {
			// don't mark job as completed because it will be started again later
			return true, false, nil
		}
	case ActionTimeout:
		// do nothing:
		// simulate a non response,
		// which will lead to a job timeout on the server side
		// don't mark job as completed
		// increment the number of failures for this job
		cptFailures := incJobFailuesCount(jobUUID)
		if cptFailures >= JobMaxFailures {
			// max number of failures reached
			// mark this job as completed, because the server will not retry this job:
			// after timeout the job will be marked as failed,
			// and because the max number of retries is reached,
			// the job will be marked as definitively failed on the server side
			return true, true, nil
		} else {
			// job will be started again later
			return true, false, nil
		}
	default:
		// same as case ActionSuccess:
		// set the job as completed
		err = SetJobAsSucceed(client, jobUUID)
		if err != nil {
			return true, false, err
		}
		// mark this job as completed
		// (will be used to count the number of completed jobs)
		return true, true, nil
	}
}

func consume(client *http.Client, func_job_consumer func(*http.Client, string) (bool, bool, error), producersDone *atomic.Bool, producerCompleted *atomic.Uint64, consumerCompleted *atomic.Uint64, cptErrors *atomic.Uint64, wgConsumer *sync.WaitGroup) {
	// this is a consumer goroutine
	defer wgConsumer.Done()

	// pull the job from the queue
	// complete the job
	for {
		// if no more jobs to consume, exit
		if producersDone.Load() && producerCompleted.Load() == consumerCompleted.Load() {
			break
		}
		found_job, job_completed, err := func_job_consumer(client, ALL_TOPICS)
		switch {
		case err != nil:
			cptErrors.Add(1)
			fmt.Println(err)
		case job_completed:
			consumerCompleted.Add(1)
		case found_job:
			// do nothing
		default:
			// no more jobs to consume (queue is empty)
			// sleep for a while to prevent cpu & network usage
			time.Sleep(50 * time.Millisecond)
		}
	}
}

/////////////////////////////////////////
// Load test functions

func showProgress(producerCompleted *atomic.Uint64, cptProducerErrors *atomic.Uint64, consumerCompleted *atomic.Uint64, cptConsumerErrors *atomic.Uint64, iterations int, done chan struct{}) {
	start := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	printProgress := func() {
		producerCompletedValue := producerCompleted.Load()
		cptProducerErrorsValue := cptProducerErrors.Load()
		consumerCompletedValue := consumerCompleted.Load()
		cptConsumerErrorsValue := cptConsumerErrors.Load()
		duration := time.Since(start)
		rate := int(float64(producerCompleted.Load()) / duration.Seconds())
		fmt.Printf("\rProgress: objective: %7d, produced: %7d, errors: %7d, consumed: %7d, errors: %7d, elapsed time: %3d sec, rate: %6d/sec\n", iterations, producerCompletedValue, cptProducerErrorsValue, consumerCompletedValue, cptConsumerErrorsValue, int(duration.Seconds()), rate)
	}

	for {
		select {
		case <-done:
			printProgress()
			return
		case <-ticker.C:
			printProgress()
		}
	}
}

func run_load_test(label string, numWorkers int, iterations int, func_job_producer func(*http.Client, string) error, func_job_generator func() string, func_job_consumer func(*http.Client, string) (bool, bool, error)) {
	fmt.Printf("Running load test: %s with %d iterations...\n", label, iterations)
	var cptProducerErrors atomic.Uint64
	var producerCompleted atomic.Uint64
	var cptConsumerErrors atomic.Uint64
	var consumerCompleted atomic.Uint64
	start := time.Now()

	// Channel to send tasks to workers
	tasks := make(chan struct{}, iterations)

	client := http.Client{
		Transport: &http.Transport{
			DisableKeepAlives:   false, // important to keep the connection alive!!! (http1.1) (improve performance > x4)
			MaxIdleConns:        min(100, numWorkers),
			MaxIdleConnsPerHost: min(100, numWorkers),
			IdleConnTimeout:     2 * time.Second,
		},
	}
	// Close the connection
	defer client.CloseIdleConnections()

	// WaitGroup to wait for all workers to finish
	var wg_producers sync.WaitGroup
	var wg_consumers sync.WaitGroup
	var producersDone atomic.Bool
	producersDone.Store(false)

	// Delete all jobs
	if *deleteAllJobsBeforeStart {
		DeleteAllJobs(&client)
	}

	// Start producers goroutines
	numProducers := max(1, numWorkers/2)
	for i := 0; i < numProducers; i++ {
		wg_producers.Add(1)
		go produce(&client, func_job_producer, func_job_generator, tasks, &producerCompleted, &cptProducerErrors, &wg_producers)
	}

	// Progress bar goroutine
	doneShowProgress := make(chan struct{})
	go showProgress(&producerCompleted, &cptProducerErrors, &consumerCompleted, &cptConsumerErrors, iterations, doneShowProgress)

	// Start consumers goroutines
	numConsumers := max(1, numWorkers/2)
	for i := 0; i < numConsumers; i++ {
		wg_consumers.Add(1)
		go consume(&client, func_job_consumer, &producersDone, &producerCompleted, &consumerCompleted, &cptConsumerErrors, &wg_consumers)
	}

	// Send tasks to workers
	for i := 0; i < iterations; i++ {
		tasks <- struct{}{}
	}
	close(tasks)

	// Wait for all producers to finish
	wg_producers.Wait()
	producersDone.Store(true)

	// Wait for all consumers to finish
	wg_consumers.Wait()

	// Stop the progress bar
	doneShowProgress <- struct{}{}

	duration := time.Since(start)
	rate := float64(producerCompleted.Load()) / duration.Seconds()
	fmt.Printf("\nLoad test completed in %s with %d success, %d errors (%d iter/sec).\n", duration, producerCompleted.Load(), cptProducerErrors.Load(), int(rate))
}

func argparse() {
	serverUrl = flag.String("url", "http://127.0.0.1:8080", "server url")
	monkeyTestLevel = flag.Int("monkey", 0, "level of monkey test")
	cptIterations = flag.Int("iterations", 1_000_000, "number of iterations")
	waitDurationSec := flag.Int("wait", 0, "wait duration in seconds")
	concurrency = flag.Int("concurrency", 1, "number of concurrent requests")
	useHTTP1_1 = flag.Bool("http1.1", false, "use HTTP/1.1 protocol")
	deleteAllJobsBeforeStart = flag.Bool("deletaAllJobsBeforeStart", false, "delete all existing jobs before start")
	flag.Parse()

	if *waitDurationSec > 0 {
		fmt.Printf("Waiting for %d seconds...\n", *waitDurationSec)
		time.Sleep(time.Duration(*waitDurationSec) * time.Second)
	}
}

func checkPing() bool {
	// make an HTTP request to the server to check if it is alive
	url := *serverUrl + "/ping"
	method := "GET"

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: false,
		},
	}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return false
	}
	req.Close = true
	// req.Header.Set("Connection", "keep-alive")

	res, err := client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	// check http response code
	if res.StatusCode != 200 {
		fmt.Println("Server is not alive.")
		return false
	}

	fmt.Println("Server is alive.")
	return true
}

func main() {
	fmt.Println("Load test client")
	argparse()
	if !checkPing() {
		fmt.Println("Exiting due to server not being alive.")
		os.Exit(-1)
	}
	fmt.Printf("Start load tests on server URL: %s\n", *serverUrl)
	run_load_test("Create jobs", *concurrency, *cptIterations, jobProducer, jobGenerator, jobConsumer)
	fmt.Printf("All load tests completed.\n")
}
