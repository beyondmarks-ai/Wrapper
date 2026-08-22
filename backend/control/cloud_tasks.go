package control

import (
	"context"
	"fmt"
	"net/http"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CloudTaskScheduler struct {
	client    *cloudtasks.Client
	queuePath string
}

func NewCloudTaskScheduler(client *cloudtasks.Client, project, location, queue string) *CloudTaskScheduler {
	return &CloudTaskScheduler{
		client:    client,
		queuePath: fmt.Sprintf("projects/%s/locations/%s/queues/%s", project, location, queue),
	}
}

func (s *CloudTaskScheduler) ScheduleExpiration(ctx context.Context, transferID, deleteURL string, expires time.Time) error {
	_, err := s.client.CreateTask(ctx, &cloudtaskspb.CreateTaskRequest{
		Parent: s.queuePath,
		Task: &cloudtaskspb.Task{
			Name:         s.queuePath + "/tasks/expire-" + transferID,
			ScheduleTime: timestamppb.New(expires),
			MessageType: &cloudtaskspb.Task_HttpRequest{HttpRequest: &cloudtaskspb.HttpRequest{
				HttpMethod: cloudtaskspb.HttpMethod_DELETE,
				Url:        deleteURL,
				Headers:    map[string]string{"User-Agent": "Wrapper-Expiry/1", "X-HTTP-Method": http.MethodDelete},
			}},
		},
	})
	return err
}
