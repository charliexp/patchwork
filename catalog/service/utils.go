package service

import (
	"fmt"
	"sync"
	"time"

	utils "github.com/patchwork-toolkit/patchwork/catalog"
)

const (
	keepaliveRetries = 5
)

// Registers service given a configured Catalog Client
func RegisterService(client CatalogClient, s *Service) error {
	_, err := client.Get(s.Id)

	if err == ErrorNotFound {
		err = client.Add(s)
		if err != nil {
			logger.Printf("Error accessing the catalog: %v\n", err)
			return err
		}
		logger.Printf("Added Service registration %v\n", s.Id)
	} else if err != nil {
		logger.Printf("Error accessing the catalog: %v\n", err)
		return err
	} else {
		err = client.Update(s.Id, s)
		if err != nil {
			logger.Printf("Error accessing the catalog: %v\n", err)
			return err
		}
		logger.Printf("Updated Service registration %v\n", s.Id)
	}
	return nil
}

// Registers service in the remote catalog
// endpoint: catalog endpoint. If empty - will be discovered using DNS-SD
// s: service registration
// sigCh: channel for shutdown signalisation from upstream
func RegisterServiceWithKeepalive(endpoint string, discover bool, s Service, sigCh <-chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	var err error
	if discover {
		endpoint, err = utils.DiscoverCatalogEndpoint(DnssdServiceType)
		if err != nil {
			logger.Printf("Error discovering endpoint: %v", err.Error())
			return
		}
	}

	// Register
	client := NewRemoteCatalogClient(endpoint)
	RegisterService(client, &s)

	// Will not keepalive registration with a negative TTL
	if s.Ttl <= 0 {
		logger.Println("Registration has ttl <= 0. Will not start the keepalive routine")
		return
	}
	logger.Printf("RegisterInRemoteCatalog (%v/%v): will update registration periodically", endpoint, s.Id)

	// Configure & start the keepalive routine
	ksigCh := make(chan bool)
	kerrCh := make(chan error)
	go keepAlive(client, &s, ksigCh, kerrCh)

	for {
		select {
		// catch an error from the keepAlive routine
		case e := <-kerrCh:
			logger.Println("Error from the keepAlive routine: ", e)
			// Re-discover the endpoint if needed and start over
			if discover {
				endpoint, err = utils.DiscoverCatalogEndpoint(DnssdServiceType)
				if err != nil {
					logger.Println("Error discovering endpoint: ", err.Error())
					return
				}
			}
			logger.Println("Will use the new endpoint: ", endpoint)
			client := NewRemoteCatalogClient(endpoint)
			RegisterService(client, &s)
			go keepAlive(client, &s, ksigCh, kerrCh)

		// catch a shutdown signal from the upstream
		case <-sigCh:
			logger.Printf("RegisterInRemoteCatalog (%v/%v): shutdown signalled by the caller", endpoint, s.Id)
			// signal shutdown to the keepAlive routine & close channels
			ksigCh <- true
			close(ksigCh)
			close(kerrCh)

			// delete entry in the remote catalog
			client.Delete(s.Id)
			return
		}
	}
}

// Keep a given registration alive
// client: configured client for the remote catalog
// s: registration to be kept alive
// sigCh: channel for shutdown signalisation from upstream
// errCh: channel for error signalisation to upstream
func keepAlive(client CatalogClient, s *Service, sigCh <-chan bool, errCh chan<- error) {
	dur := utils.KeepAliveDuration(s.Ttl)
	ticker := time.NewTicker(dur)
	errTries := 0

	for {
		select {
		case <-ticker.C:
			err := client.Update(s.Id, s)

			if err == ErrorNotFound {
				logger.Printf("Registration %v not found in the remote catalog. TTL expired?\n", s.Id)
				err = client.Add(s)
				if err != nil {
					logger.Printf("Error accessing the catalog: %v\n", err)
					errTries += 1
				} else {
					logger.Printf("Added Service registration %v\n", s.Id)
					errTries = 0
				}
			} else if err != nil {
				logger.Printf("Error accessing the catalog: %v\n", err)
				errTries += 1
			} else {
				logger.Printf("Updated Service registration %v\n", s.Id)
				errTries = 0
			}
			if errTries >= keepaliveRetries {
				errCh <- fmt.Errorf("Number of retries exceeded")
				ticker.Stop()
				return
			}
		case <-sigCh:
			// logger.Println("keepAlive routine shutdown signalled by the upstream")
			return
		}
	}
}
