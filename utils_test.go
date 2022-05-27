package fake_kubelet

import (
	"reflect"
	"testing"
	"time"
)

func TestParallelTasks(t *testing.T) {
	tasks := newParallelTasks(4)
	startTime := time.Now()
	for i := 0; i < 10; i++ {
		tasks.Add(func() {
			time.Sleep(1 * time.Second)
		})
	}

	tasks.Wait()
	elapsed := time.Since(startTime)
	if elapsed >= 4*time.Second {
		t.Fatalf("Tasks took too long to complete: %v", elapsed)
	} else if elapsed < 3*time.Second {
		t.Fatalf("Tasks completed too quickly: %v", elapsed)
	}
	t.Logf("Tasks completed in %v", elapsed)
}

func Test_modifyStatusByAnnotations(t *testing.T) {
	type args struct {
		origin []byte
		anno   map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			args: args{
				origin: []byte(`{"phase":"Running"}`),
				anno: map[string]string{
					"fake/status.phase": "Succeeded",
				},
			},
			want: []byte(`{"phase":"Succeeded"}`),
		},
		{
			args: args{
				origin: []byte(`{}`),
				anno: map[string]string{
					"fake/status.phase": "Succeeded",
				},
			},
			want: []byte(`{"phase":"Succeeded"}`),
		},
		{
			args: args{
				origin: []byte(`{"containerStatuses":[{}]}`),
				anno: map[string]string{
					"fake/status.phase":                     `"Running"`,
					"fake/status.containerStatuses.0.ready": "true",
				},
			},
			want: []byte(`{"containerStatuses":[{"ready":true}],"phase":"Running"}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := modifyStatusByAnnotations(tt.args.origin, tt.args.anno)
			if (err != nil) != tt.wantErr {
				t.Errorf("modifyStatusByAnnotations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("modifyStatusByAnnotations() got = %q, want %q", got, tt.want)
			}
		})
	}
}
