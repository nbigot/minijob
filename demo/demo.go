package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/jobbackendprovider/mockprovider"
	"go.uber.org/zap"
)

/////////////////////////////////////////////////////////////////////

type Service struct {
	// implements IService interface
	notifChanBP chan jobbackendprovider.Event // notification channel for job backend provider
	// notifChanSvc chan ServiceEvent                      // notification channel for metrics
	wg       sync.WaitGroup                         // wg is a wait group to wait for the Run function to finish
	stopChan chan struct{}                          // stopChan is a channel to stop the Run function
	running  atomic.Bool                            // Add this to track if the service is running
	bp       jobbackendprovider.IJobBackendProvider // backend provider
}

func (svc *Service) Init() error {
	var err error

	if err = svc.bp.Init(svc.notifChanBP); err != nil {
		return err
	}

	fmt.Println("Service Init")

	return nil
}

func (svc *Service) Stop() error {
	if !svc.running.Load() {
		return errors.New("service is not running")
	}

	// send stop notification
	close(svc.stopChan)
	// wait for the Run function to finish
	svc.wg.Wait()
	return nil
}

func (svc *Service) Run() error {
	if !svc.running.CompareAndSwap(false, true) {
		return errors.New("service is already running")
	}
	defer svc.running.Store(false)

	defer func() {
		fmt.Println("Service stopped")
	}()

	// this function must be called in a goroutine
	svc.wg.Add(1)
	defer svc.wg.Done()

	fmt.Println("Service started")

	// Run the job backend provider in a separate goroutine
	go svc.bp.Run()

	for {
		select {
		case <-svc.stopChan:
			// Channel was closed, time to stop
			// received a signal to stop the service
			// stop and wait for the job backend provider to end (synchronous)
			svc.bp.Stop()
			// stop the service itself
			// must be ok (synchronous)
			// svc.wg.Wait()
			// stop the main goroutine (the service is stopped)
			return nil
		case event := <-svc.notifChanBP:
			fmt.Println("Event received")
			switch event.Type {
			case jobbackendprovider.EventFatalError:
				fmt.Println("Fatal error received from job backend provider")
				// stop and wait for the job backend provider to end (synchronous)
				svc.bp.Stop()
				// stop the main goroutine (the service is stopped)
				return errors.New("job backend provider received a fatal error")
			default:
				// do nothing
			}
		}
	}
}

func NewService() (*Service, error) {
	bp, err := mockprovider.NewMockJobBackendProvider(zap.NewNop(), nil)
	if err != nil {
		return nil, err
	}
	return &Service{
		bp:          bp,
		notifChanBP: make(chan jobbackendprovider.Event, 100),
		// notifChanSvc: make(chan ServiceEvent, 100),
		stopChan: make(chan struct{}),
	}, nil
}

func CreateAndInitService() (*Service, error) {
	var err error

	svc, err := NewService()
	if err != nil {
		fmt.Println("Error while instantiate job service")
		return nil, err
	}

	err = svc.Init()
	if err != nil {
		fmt.Println("Error while initialize stream service")
		return nil, err
	}

	fmt.Println("Job server started")

	return svc, nil
}

// add main function
func main() {
	fmt.Println("Starting...")
	svc, err := CreateAndInitService()
	if err != nil {
		fmt.Println("Error while creating and initializing service")
		return
	}
	go svc.Run()
	time.Sleep(5 * time.Second)
	fmt.Println("Stopping service...")
	svc.Stop()
	fmt.Println("Done.")
}
