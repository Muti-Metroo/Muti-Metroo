// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/filetransfer"
)

// newFileTransferTestChain creates a 4-agent chain with file transfer enabled on agent D
// and HTTP server enabled on agent A.
func newFileTransferTestChain(t *testing.T, ftCfg *config.FileTransferConfig) *AgentChain {
	chain := NewAgentChain(t)
	chain.EnableHTTP = true
	chain.FileTransferConfig = ftCfg
	return chain
}

// uploadResponse is the JSON response from file upload.
type uploadResponse struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	BytesWritten int64  `json:"bytes_written"`
	RemotePath   string `json:"remote_path,omitempty"`
}

// downloadRequest is the JSON request for file download.
type downloadRequest struct {
	Path         string `json:"path"`
	RateLimit    int64  `json:"rate_limit,omitempty"`
	Offset       int64  `json:"offset,omitempty"`
	OriginalSize int64  `json:"original_size,omitempty"`
}

// uploadFile uploads a file via the HTTP API. Authorization is handled by
// the gateway agent: it signs the outgoing TransferMetadata with the
// management signing key that the test chain installed on every node.
func uploadFile(t *testing.T, agentAddr, targetID, localPath, remotePath string) (*uploadResponse, error) {
	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", filepath.Base(localPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	// Add path
	if err := writer.WriteField("path", remotePath); err != nil {
		return nil, fmt.Errorf("failed to write path field: %w", err)
	}

	// Add directory flag
	if err := writer.WriteField("directory", fmt.Sprintf("%v", info.IsDir())); err != nil {
		return nil, fmt.Errorf("failed to write directory field: %w", err)
	}

	writer.Close()

	// Send request
	url := fmt.Sprintf("http://%s/agents/%s/file/upload", agentAddr, targetID)
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result uploadResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
	}

	return &result, nil
}

// downloadFile downloads a file via the HTTP API.
func downloadFile(t *testing.T, agentAddr, targetID, remotePath, localPath string, rateLimit, offset, originalSize int64) error {
	reqBody := downloadRequest{
		Path:         remotePath,
		RateLimit:    rateLimit,
		Offset:       offset,
		OriginalSize: originalSize,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/agents/%s/file/download", agentAddr, targetID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Create local file
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// TestFileTransfer_BasicUploadDownload tests basic file upload and download.
func TestFileTransfer_BasicUploadDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir}, // Allow only temp directory
		MaxFileSize:  0,                // Unlimited
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for routes to propagate
	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create test file
	testContent := []byte("Hello, file transfer integration test!")
	localPath := filepath.Join(t.TempDir(), "test-upload.txt")
	if err := os.WriteFile(localPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	remotePath := filepath.Join(tmpDir, "test-file.txt")

	// Upload file
	t.Log("Uploading file...")
	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, remotePath)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Upload not successful: %s", result.Error)
	}
	t.Logf("Uploaded %d bytes", result.BytesWritten)

	// Verify file exists on remote
	if _, err := os.Stat(remotePath); os.IsNotExist(err) {
		t.Fatal("Remote file does not exist after upload")
	}

	// Download file
	downloadPath := filepath.Join(t.TempDir(), "test-download.txt")
	t.Log("Downloading file...")
	if err := downloadFile(t, chain.HTTPAddrs[0], targetID, remotePath, downloadPath, 0, 0, 0); err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify downloaded content
	downloadedContent, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if !bytes.Equal(testContent, downloadedContent) {
		t.Errorf("Content mismatch: expected %q, got %q", string(testContent), string(downloadedContent))
	} else {
		t.Log("File content verified successfully")
	}
}

// TestFileTransfer_LargeFile tests uploading and downloading a large file.
func TestFileTransfer_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
		MaxFileSize:  0, // Unlimited
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create a 1MB test file with random content
	fileSize := 1024 * 1024 // 1MB
	testContent := make([]byte, fileSize)
	if _, err := rand.Read(testContent); err != nil {
		t.Fatalf("Failed to generate random data: %v", err)
	}

	localPath := filepath.Join(t.TempDir(), "large-file.bin")
	if err := os.WriteFile(localPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	remotePath := filepath.Join(tmpDir, "large-file.bin")

	// Upload
	t.Logf("Uploading %d bytes...", fileSize)
	start := time.Now()
	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, remotePath)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Upload not successful: %s", result.Error)
	}
	uploadDuration := time.Since(start)
	t.Logf("Upload completed in %v (%d bytes/s)", uploadDuration, int64(float64(fileSize)/uploadDuration.Seconds()))

	// Calculate checksum of uploaded file
	uploadedContent, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}
	uploadHash := sha256.Sum256(uploadedContent)

	// Download
	downloadPath := filepath.Join(t.TempDir(), "large-file-downloaded.bin")
	t.Log("Downloading file...")
	start = time.Now()
	if err := downloadFile(t, chain.HTTPAddrs[0], targetID, remotePath, downloadPath, 0, 0, 0); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	downloadDuration := time.Since(start)
	t.Logf("Download completed in %v", downloadDuration)

	// Verify checksum
	downloadedContent, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	downloadHash := sha256.Sum256(downloadedContent)

	if uploadHash != downloadHash {
		t.Error("Checksum mismatch between original and downloaded file")
	} else {
		t.Logf("Large file transfer verified: %s", hex.EncodeToString(downloadHash[:8]))
	}
}

// TestFileTransfer_SignatureRequired verifies that when the origin agent
// has no management signing private key, uploads are rejected before
// reaching the remote. This guarantees "no management key, no permissions".
func TestFileTransfer_SignatureRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	// Strip private key from the gateway agent (A) so it cannot sign.
	chain.ManagementConfigure = func(idx int, mgmt *config.ManagementConfig) {
		if idx == 0 {
			mgmt.SigningPrivateKey = ""
		}
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	localPath := filepath.Join(t.TempDir(), "auth-test.txt")
	if err := os.WriteFile(localPath, []byte("nope"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	remotePath := filepath.Join(tmpDir, "auth-test.txt")

	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, remotePath)
	if err == nil && result != nil && result.Success {
		t.Fatal("Upload should have failed without gateway private key")
	}
	if result != nil && result.Error != "" {
		t.Logf("Got expected failure: %v", result.Error)
	}
}

// TestFileTransfer_TargetMissingPublicKey verifies that a target with no
// configured public key refuses every upload/download.
func TestFileTransfer_TargetMissingPublicKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.ManagementConfigure = func(idx int, mgmt *config.ManagementConfig) {
		if idx == 3 {
			mgmt.SigningPublicKey = ""
		}
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	localPath := filepath.Join(t.TempDir(), "auth-test.txt")
	if err := os.WriteFile(localPath, []byte("nope"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	remotePath := filepath.Join(tmpDir, "auth-test.txt")

	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, remotePath)
	if err == nil && result != nil && result.Success {
		t.Fatal("Upload should have failed when target has no public key")
	}
}

// TestFileTransfer_PathRestrictions tests that only allowed paths can be accessed.
func TestFileTransfer_PathRestrictions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	allowedDir := t.TempDir()
	restrictedDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{allowedDir}, // Only allow one directory
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create test file
	testContent := []byte("Path restriction test")
	localPath := filepath.Join(t.TempDir(), "path-test.txt")
	if err := os.WriteFile(localPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test 1: Upload to allowed path should succeed
	t.Log("Testing upload to allowed path...")
	allowedRemotePath := filepath.Join(allowedDir, "allowed-file.txt")
	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, allowedRemotePath)
	if err != nil {
		t.Fatalf("Upload to allowed path failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Upload to allowed path not successful: %s", result.Error)
	}
	t.Log("Upload to allowed path succeeded")

	// Test 2: Upload to restricted path should fail
	t.Log("Testing upload to restricted path (should fail)...")
	restrictedRemotePath := filepath.Join(restrictedDir, "restricted-file.txt")
	result, err = uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, restrictedRemotePath)
	if err == nil && result != nil && result.Success {
		t.Error("Upload to restricted path should have failed")
	} else {
		t.Logf("Got expected failure for restricted path")
	}

	// Test 3: Download from restricted path should fail
	// First create a file in the restricted directory
	restrictedFile := filepath.Join(restrictedDir, "secret.txt")
	if err := os.WriteFile(restrictedFile, []byte("secret content"), 0644); err != nil {
		t.Fatalf("Failed to create restricted file: %v", err)
	}

	downloadPath := filepath.Join(t.TempDir(), "should-not-exist.txt")
	t.Log("Testing download from restricted path (should fail)...")
	if err := downloadFile(t, chain.HTTPAddrs[0], targetID, restrictedFile, downloadPath, 0, 0, 0); err == nil {
		if _, statErr := os.Stat(downloadPath); statErr == nil {
			t.Error("Download from restricted path should have failed")
		}
	} else {
		t.Logf("Got expected failure for restricted download")
	}
}

// TestFileTransfer_MaxFileSize tests the max file size limit.
func TestFileTransfer_MaxFileSize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	maxSize := int64(1024) // 1KB limit

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
		MaxFileSize:  maxSize,
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Test 1: File within limit should succeed
	t.Log("Testing file within size limit...")
	smallContent := make([]byte, 500) // 500 bytes
	rand.Read(smallContent)
	smallPath := filepath.Join(t.TempDir(), "small-file.bin")
	if err := os.WriteFile(smallPath, smallContent, 0644); err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	remotePath := filepath.Join(tmpDir, "small-file.bin")
	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, smallPath, remotePath)
	if err != nil {
		t.Fatalf("Upload of small file failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Upload of small file not successful: %s", result.Error)
	}
	t.Log("Small file upload succeeded")

	// Test 2: File exceeding limit should fail
	t.Log("Testing file exceeding size limit (should fail)...")
	largeContent := make([]byte, 2048) // 2KB - exceeds limit
	rand.Read(largeContent)
	largePath := filepath.Join(t.TempDir(), "large-file.bin")
	if err := os.WriteFile(largePath, largeContent, 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	largeRemotePath := filepath.Join(tmpDir, "large-file.bin")
	result, err = uploadFile(t, chain.HTTPAddrs[0], targetID, largePath, largeRemotePath)
	if err == nil && result != nil && result.Success {
		t.Error("Upload of file exceeding size limit should have failed")
	} else {
		t.Logf("Got expected failure for oversized file")
	}
}

// TestFileTransfer_ResumeDownload tests resumable downloads using partial files.
func TestFileTransfer_ResumeDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create a test file on the "remote" (agent D's allowed path)
	fileSize := int64(10000) // 10KB
	testContent := make([]byte, fileSize)
	for i := range testContent {
		testContent[i] = byte(i % 256) // Predictable pattern
	}

	remotePath := filepath.Join(tmpDir, "resume-test.bin")
	if err := os.WriteFile(remotePath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create remote file: %v", err)
	}

	// Download first half
	offset := int64(5000) // Start from middle

	t.Logf("Downloading from offset %d...", offset)
	downloadPath := filepath.Join(t.TempDir(), "resume-download.bin")

	// Download with offset
	if err := downloadFile(t, chain.HTTPAddrs[0], targetID, remotePath, downloadPath, 0, offset, fileSize); err != nil {
		t.Fatalf("Resume download failed: %v", err)
	}

	// Verify we got the second half
	downloadedContent, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	expectedContent := testContent[offset:]
	if !bytes.Equal(expectedContent, downloadedContent) {
		t.Errorf("Content mismatch: expected %d bytes from offset %d, got %d bytes",
			len(expectedContent), offset, len(downloadedContent))
	} else {
		t.Logf("Resume download verified: got %d bytes from offset %d", len(downloadedContent), offset)
	}
}

// TestFileTransfer_RateLimit tests rate-limited transfers.
func TestFileTransfer_RateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      true,
		AllowedPaths: []string{tmpDir},
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create a 50KB test file on the remote
	fileSize := int64(50 * 1024) // 50KB
	testContent := make([]byte, fileSize)
	rand.Read(testContent)

	remotePath := filepath.Join(tmpDir, "ratelimit-test.bin")
	if err := os.WriteFile(remotePath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create remote file: %v", err)
	}

	// Download with rate limit of 10KB/s - should take at least 5 seconds
	rateLimit := int64(10 * 1024) // 10 KB/s

	t.Logf("Downloading %d bytes with rate limit %d bytes/s...", fileSize, rateLimit)
	downloadPath := filepath.Join(t.TempDir(), "ratelimit-download.bin")

	start := time.Now()
	if err := downloadFile(t, chain.HTTPAddrs[0], targetID, remotePath, downloadPath, rateLimit, 0, 0); err != nil {
		t.Fatalf("Rate-limited download failed: %v", err)
	}
	duration := time.Since(start)

	// Expected minimum time considering rate limiter's 16KB burst allowance:
	// - First 16KB comes through instantly (burst)
	// - Remaining (fileSize - 16KB) is rate-limited
	// With 0.8 tolerance factor for timing variations
	burstSize := int64(16 * 1024) // Rate limiter burst size
	rateLimitedBytes := fileSize - burstSize
	if rateLimitedBytes < 0 {
		rateLimitedBytes = 0
	}
	expectedMinDuration := time.Duration(float64(rateLimitedBytes)/float64(rateLimit)*0.8) * time.Second
	if duration < expectedMinDuration {
		t.Errorf("Download completed too fast: %v (expected at least %v with rate limit)", duration, expectedMinDuration)
	} else {
		actualRate := float64(fileSize) / duration.Seconds()
		t.Logf("Rate-limited download completed in %v (%.1f KB/s, limit was %.1f KB/s)",
			duration, actualRate/1024, float64(rateLimit)/1024)
	}

	// Verify file content
	downloadedContent, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if !bytes.Equal(testContent, downloadedContent) {
		t.Error("Content mismatch after rate-limited download")
	}
}

// TestFileTransfer_PartialFileTracking tests the partial file tracking mechanism.
func TestFileTransfer_PartialFileTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test the filetransfer.PartialInfo functionality directly
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test-file.bin")

	// Create partial file
	originalSize := int64(10000)
	f, err := filetransfer.CreatePartialFile(testPath, originalSize, "/remote/path", 0644)
	if err != nil {
		t.Fatalf("Failed to create partial file: %v", err)
	}

	// Write some data
	data := make([]byte, 5000)
	rand.Read(data)
	if _, err := f.Write(data); err != nil {
		t.Fatalf("Failed to write to partial file: %v", err)
	}
	f.Close()

	// Update progress
	if err := filetransfer.UpdatePartialProgress(testPath, 5000); err != nil {
		t.Fatalf("Failed to update partial progress: %v", err)
	}

	// Check partial file exists
	info, err := filetransfer.HasPartialFile(testPath)
	if err != nil {
		t.Fatalf("Failed to check partial file: %v", err)
	}
	if info == nil {
		t.Fatal("Expected partial file info, got nil")
	}

	t.Logf("Partial file info: OriginalSize=%d, BytesWritten=%d", info.OriginalSize, info.BytesWritten)

	if info.OriginalSize != originalSize {
		t.Errorf("OriginalSize mismatch: expected %d, got %d", originalSize, info.OriginalSize)
	}
	if info.BytesWritten != 5000 {
		t.Errorf("BytesWritten mismatch: expected 5000, got %d", info.BytesWritten)
	}

	// Finalize partial file
	if err := filetransfer.FinalizePartial(testPath, 0644); err != nil {
		t.Fatalf("Failed to finalize partial file: %v", err)
	}

	// Verify final file exists
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("Final file does not exist after finalization")
	}

	// Verify partial files are cleaned up
	partialPath := filetransfer.GetPartialPath(testPath)
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error("Partial file still exists after finalization")
	}

	infoPath := filetransfer.GetPartialInfoPath(testPath)
	if _, err := os.Stat(infoPath); !os.IsNotExist(err) {
		t.Error("Partial info file still exists after finalization")
	}

	t.Log("Partial file tracking test passed")
}

// TestFileTransfer_RateLimitReader tests the rate-limited reader directly.
func TestFileTransfer_RateLimitReader(t *testing.T) {
	// Test that rate limiting works at the reader level
	// The rate limiter has a 16KB burst, so we need data larger than 16KB
	// to actually trigger rate limiting after the initial burst
	data := make([]byte, 32*1024) // 32KB (16KB burst + 16KB to rate limit)
	rand.Read(data)

	// Create rate-limited reader at 8KB/s
	// After 16KB burst, remaining 16KB should take ~2 seconds
	rateLimit := int64(8 * 1024)
	ctx := context.Background()
	reader := filetransfer.NewRateLimitedReader(ctx, bytes.NewReader(data), rateLimit)

	// Read all data and measure time
	start := time.Now()
	result, err := io.ReadAll(reader)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to read from rate-limited reader: %v", err)
	}

	if !bytes.Equal(data, result) {
		t.Error("Data mismatch from rate-limited reader")
	}

	// After the 16KB burst, we have 16KB remaining at 8KB/s = 2 seconds
	// Use 1 second threshold to avoid flaky tests
	expectedMinDuration := 1 * time.Second
	if duration < expectedMinDuration {
		t.Errorf("Rate-limited read completed too fast: %v (expected at least %v)", duration, expectedMinDuration)
	} else {
		t.Logf("Rate-limited read took %v for %d bytes", duration, len(data))
	}
}

// TestFileTransfer_ZeroRateLimitIsUnlimited tests that rate limit of 0 means unlimited.
func TestFileTransfer_ZeroRateLimitIsUnlimited(t *testing.T) {
	data := make([]byte, 100*1024) // 100KB
	rand.Read(data)

	// Create reader with 0 rate limit (should be unlimited)
	ctx := context.Background()
	reader := filetransfer.NewRateLimitedReader(ctx, bytes.NewReader(data), 0)

	// Read all data - should be very fast
	start := time.Now()
	result, err := io.ReadAll(reader)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if !bytes.Equal(data, result) {
		t.Error("Data mismatch")
	}

	// Should complete very quickly (under 100ms for 100KB in memory)
	if duration > 500*time.Millisecond {
		t.Errorf("Unlimited read took too long: %v", duration)
	} else {
		t.Logf("Unlimited read took %v for %d bytes", duration, len(data))
	}
}

// TestFileTransfer_Disabled tests that file transfer is properly disabled.
func TestFileTransfer_Disabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	ftCfg := &config.FileTransferConfig{
		Enabled:      false, // Disabled
		AllowedPaths: []string{tmpDir},
	}

	chain := newFileTransferTestChain(t, ftCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	chain.WaitForAgentPath(t, 0, 3)

	targetID := chain.Agents[3].ID().String()

	// Create test file
	testContent := []byte("Should not upload")
	localPath := filepath.Join(t.TempDir(), "disabled-test.txt")
	if err := os.WriteFile(localPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	remotePath := filepath.Join(tmpDir, "disabled-file.txt")

	// Upload should fail when disabled
	t.Log("Testing upload when file transfer is disabled...")
	result, err := uploadFile(t, chain.HTTPAddrs[0], targetID, localPath, remotePath)
	if err == nil && result != nil && result.Success {
		t.Error("Upload should have failed when file transfer is disabled")
	} else {
		if result != nil && strings.Contains(result.Error, "disabled") {
			t.Logf("Got expected error: %s", result.Error)
		} else if err != nil {
			t.Logf("Got error: %v", err)
		} else {
			t.Log("Upload failed as expected")
		}
	}
}
