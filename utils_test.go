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

func TestGenerateSerialNumber(t *testing.T) {
	type args struct {
		n      int
		minLen int
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			args: args{
				n:      0,
				minLen: 2,
			},
			want: []string{},
		},
		{
			args: args{
				n:      1,
				minLen: 2,
			},
			want: []string{"00"},
		},
		{
			args: args{
				n:      1,
				minLen: 1,
			},
			want: []string{"0"},
		},
		{
			args: args{
				n:      100,
				minLen: 2,
			},
			want: []string{"00", "01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25", "26", "27", "28", "29", "30", "31", "32", "33", "34", "35", "36", "37", "38", "39", "40", "41", "42", "43", "44", "45", "46", "47", "48", "49", "50", "51", "52", "53", "54", "55", "56", "57", "58", "59", "60", "61", "62", "63", "64", "65", "66", "67", "68", "69", "70", "71", "72", "73", "74", "75", "76", "77", "78", "79", "80", "81", "82", "83", "84", "85", "86", "87", "88", "89", "90", "91", "92", "93", "94", "95", "96", "97", "98", "99"},
		},
		{
			args: args{
				n:      101,
				minLen: 2,
			},
			want: []string{"00", "01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25", "26", "27", "28", "29", "30", "31", "32", "33", "34", "35", "36", "37", "38", "39", "40", "41", "42", "43", "44", "45", "46", "47", "48", "49", "50", "51", "52", "53", "54", "55", "56", "57", "58", "59", "60", "61", "62", "63", "64", "65", "66", "67", "68", "69", "70", "71", "72", "73", "74", "75", "76", "77", "78", "79", "80", "81", "82", "83", "84", "85", "86", "87", "88", "89", "90", "91", "92", "93", "94", "95", "96", "97", "98", "99", "100"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			GenerateSerialNumber(tt.args.n, tt.args.minLen, func(s string) bool {
				got = append(got, s)
				return true
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateSerialNumber() got = %q, want %q", got, tt.want)
			}
		})
	}
}
