package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

const (
	High int = 1
	Low      = 2
)

func setCPUFrequency(frequency int) error {
	m := map[int]string{
		High: "performance",
		Low:  "shared",
	}
	command := fmt.Sprintf("kubectl apply -f - <<EOF\napiVersion: \"power.intel.com/v1\"\nkind: PowerWorkload\nmetadata:\n  # Replace <NODE_NAME> with the Node you intend this PowerWorkload to be associated with\n  name: shared-node-1.kt-cluster.ntu-cloud-pg0.utah.cloudlab.us-workload\n  namespace: intel-power\nspec:\n  # Replace <NODE_NAME> with the Node you intend this PowerWorkload to be associated with\n  name: \"shared-node-1.kt-cluster.ntu-cloud-pg0.utah.cloudlab.us-workload\"\n  allCores: true\n  powerNodeSelector:\n    # The label must be as below, as this workload will be specific to the Node\n    kubernetes.io/hostname: node-1.kt-cluster.ntu-cloud-pg0.utah.cloudlab.us\n powerProfile: \"%s\"\nEOF", m[frequency])
	cmd := exec.Command("bash", "-c", command)

	// Capture and check for any errors.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	fmt.Println(output)
	return nil
}

func main() {
	// Define your Prometheus query and threshold values
	command := "curl -sG 'http://127.0.0.1:9090/api/v1/query?' --data-urlencode 'query=(avg by(instance) (rate(node_cpu_seconds_total{mode=\"idle\"}[2m])) * 100)' | jq -r '.data.result[1].value[1]'"
	//thresholdHigh := 80.0 // Mostly idle => decrease frequency
	//thresholdLow := 20.0  // Mostly CPU bound => increase frequency

	for {
		cmd := exec.Command("bash", "-c", command)

		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf(fmt.Sprintf("ERR :%+v", err))
		}
		fmt.Println(string(output))
		// Parse the string to a float64
		floatValue, err := strconv.ParseFloat(string(output), 64)
		if err != nil {
			fmt.Printf("Error converting to float: %v\n", err)
			return
		}
		fmt.Println(floatValue)

		//if metricValue > thresholdHigh {
		//	if err := setCPUFrequency(Low); err != nil {
		//		fmt.Println("Failed to set low CPU frequency:", err)
		//	}
		//} else if metricValue != 0 && metricValue < thresholdLow {
		//	if err := setCPUFrequency(High); err != nil {
		//		fmt.Println("Failed to set high CPU frequency:", err)
		//	}
		//}

		time.Sleep(60 * time.Second) // Adjust the polling interval as needed
	}
}