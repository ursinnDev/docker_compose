/*
   Copyright 2020 Docker Compose CLI authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package compose

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// LogPrinter watch application containers an collect their logs
type LogPrinter interface {
	HandleEvent(event ContainerEvent)
	Run(cascadeStop bool, exitCodeFrom string, stopFn func() error) (int, error)
	Cancel()
}

// NewLogPrinter builds a LogPrinter passing containers logs to LogConsumer
func NewLogPrinter(consumer LogConsumer) LogPrinter {
	queue := make(chan ContainerEvent)
	printer := printer{
		consumer: consumer,
		queue:    queue,
	}
	return &printer
}

func (p *printer) Cancel() {
	p.queue <- ContainerEvent{
		Type: UserCancel,
	}
}

type printer struct {
	queue    chan ContainerEvent
	consumer LogConsumer
}

func (p *printer) HandleEvent(event ContainerEvent) {
	p.queue <- event
}

func (p *printer) Run(cascadeStop bool, exitCodeFrom string, stopFn func() error) (int, error) {
	var (
		aborting bool
		exitCode int
	)
	containers := map[string]struct{}{}
	for {
		event := <-p.queue
		container := event.Container
		switch event.Type {
		case UserCancel:
			aborting = true
		case ContainerEventAttach:
			if _, ok := containers[container]; ok {
				continue
			}
			containers[container] = struct{}{}
			p.consumer.Register(container)
		case ContainerEventExit:
			if !event.Restarting {
				delete(containers, container)
			}
			if !aborting {
				p.consumer.Status(container, fmt.Sprintf("exited with code %d", event.ExitCode))
			}
			if cascadeStop {
				if !aborting {
					aborting = true
					fmt.Println("Aborting on container exit...")
					err := stopFn()
					if err != nil {
						return 0, err
					}
				}
				if exitCodeFrom == "" {
					exitCodeFrom = event.Service
				}
				if exitCodeFrom == event.Service {
					logrus.Error(event.ExitCode)
					exitCode = event.ExitCode
				}
			}
			if len(containers) == 0 {
				// Last container terminated, done
				return exitCode, nil
			}
		case ContainerEventLog:
			if !aborting {
				p.consumer.Log(container, event.Service, event.Line)
			}
		}
	}
}