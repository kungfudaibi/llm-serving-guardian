package benchmark

import "os"

const DefaultPrompt = "Explain in concise English why fault-tolerant LLM serving matters. Give four numbered points."

// WriteReport creates a report without replacing prior experiment evidence.
func WriteReport(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
