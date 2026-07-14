package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/connection"
	"github.com/spf13/cobra"
)

type fileCommandRuntime struct {
	socket   *string
	logFile  *string
	timeout  time.Duration
	endpoint string
}

const maxCLIFileChunkBytes = 1 << 20

type fileEntryView struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       uint32 `json:"mode"`
	ModifiedAt string `json:"modified_at,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type fileOperationView struct {
	Path       string `json:"path"`
	TargetPath string `json:"target_path,omitempty"`
	Success    bool   `json:"success"`
	ErrorCode  string `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type fileTransferView struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	EndpointID    string `json:"endpoint_id"`
	RemotePath    string `json:"remote_path"`
	LocalPath     string `json:"local_path,omitempty"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
}

func newFileCommand(socket, logFile *string) *cobra.Command {
	runtime := &fileCommandRuntime{socket: socket, logFile: logFile}
	command := &cobra.Command{Use: "file", Short: "Operate on an endpoint daemon file system"}
	command.PersistentFlags().DurationVar(&runtime.timeout, "timeout", 2*time.Minute, "operation timeout")
	command.AddCommand(
		newFileListCommand(runtime), newFileStatCommand(runtime), newFileCatCommand(runtime),
		newFileDownloadCommand(runtime), newFileUploadCommand(runtime), newFileMkdirCommand(runtime),
		newFileRenameCommand(runtime), newFileCopyMoveCommand(runtime, false), newFileCopyMoveCommand(runtime, true),
		newFileRemoveCommand(runtime),
	)
	return command
}

func (runtime *fileCommandRuntime) open(ctx context.Context, cmd *cobra.Command, endpointValue string) (*protocol.Client, connection.Config, func(), error) {
	if runtime.timeout <= 0 {
		return nil, connection.Config{}, func() {}, usageCLIError("--timeout must be positive")
	}
	registry, err := loadNormalizedConnectionRegistry()
	if err != nil {
		return nil, connection.Config{}, func() {}, err
	}
	endpoint, err := resolveEndpointConfig(endpointValue, registry)
	if err != nil {
		return nil, connection.Config{}, func() {}, err
	}
	cmd.Root().SilenceUsage = true
	client, closeClient, err := openEndpointProtocolClient(ctx, endpoint, *runtime.socket, *runtime.logFile)
	if err != nil {
		return nil, connection.Config{}, func() {}, classifyCLIError(err)
	}
	return client, endpoint, closeClient, nil
}

func (runtime *fileCommandRuntime) context(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), runtime.timeout)
}

func newFileListCommand(runtime *fileCommandRuntime) *cobra.Command {
	var cursor string
	var limit int
	var all, jsonOutput bool
	command := &cobra.Command{
		Use: "list ENDPOINT [PATH]", Short: "List a daemon-owned directory", Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 || limit > 10000 {
				return usageCLIError("--limit must be 1..10000")
			}
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			path := ""
			if len(args) == 2 {
				path = args[1]
			} else {
				defaults, err := client.PathDefaults(ctx)
				if err != nil {
					return classifyCLIError(err)
				}
				path = defaults.DefaultCWD
			}
			entries := make([]fileEntryView, 0)
			next := cursor
			seen := make(map[string]struct{})
			for {
				result, err := client.FileList(ctx, protocol.FileListParams{Path: path, Cursor: next, Limit: limit})
				if err != nil {
					return classifyCLIError(err)
				}
				for _, entry := range result.Entries {
					entries = append(entries, fileEntryProjection(entry))
				}
				next = result.NextCursor
				if !all || next == "" {
					break
				}
				if _, duplicate := seen[next]; duplicate {
					return &cliError{code: 6, message: "daemon returned a repeated file cursor"}
				}
				seen[next] = struct{}{}
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int             `json:"schema_version"`
					Kind          string          `json:"kind"`
					EndpointID    string          `json:"endpoint_id"`
					Path          string          `json:"path"`
					Items         []fileEntryView `json:"items"`
					NextCursor    string          `json:"next_cursor,omitempty"`
				}{1, "file_list", string(endpoint.ID), path, entries, next})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "TYPE\tSIZE\tMODIFIED\tNAME")
			for _, entry := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d\t%s\t%s\n", entry.Type, entry.Size, entry.ModifiedAt, entry.Name)
			}
			return nil
		},
	}
	command.Flags().StringVar(&cursor, "cursor", "", "daemon-issued pagination cursor")
	command.Flags().IntVar(&limit, "limit", 200, "entries per page")
	command.Flags().BoolVar(&all, "all", false, "read all pages")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileStatCommand(runtime *fileCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "stat ENDPOINT PATH", Short: "Show daemon-owned file metadata", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			entry, err := client.FileStat(ctx, args[1])
			if err != nil {
				return classifyCLIError(err)
			}
			view := fileEntryProjection(*entry)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int           `json:"schema_version"`
					Kind          string        `json:"kind"`
					EndpointID    string        `json:"endpoint_id"`
					Item          fileEntryView `json:"item"`
				}{1, "file_stat", string(endpoint.ID), view})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\nType: %s\nSize: %d\nMode: %04o\nModified: %s\n", view.Path, view.Type, view.Size, view.Mode, view.ModifiedAt)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileCatCommand(runtime *fileCommandRuntime) *cobra.Command {
	return &cobra.Command{
		Use: "cat ENDPOINT PATH", Short: "Stream a daemon-owned file to stdout", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, _, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			_, err = downloadEndpointFile(ctx, client, args[1], cmd.OutOrStdout())
			return classifyCLIError(err)
		},
	}
}

func newFileDownloadCommand(runtime *fileCommandRuntime) *cobra.Command {
	var overwrite, jsonOutput bool
	command := &cobra.Command{
		Use: "download ENDPOINT REMOTE [LOCAL]", Short: "Download and verify a daemon-owned file", Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			localPath := filepath.Base(args[1])
			if len(args) == 3 {
				localPath = args[2]
			}
			result, err := downloadEndpointFileAtomic(ctx, client, args[1], localPath, overwrite)
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					return &cliError{code: 4, message: fmt.Sprintf("local file %s already exists; use --overwrite", localPath), cause: err}
				}
				return classifyCLIError(err)
			}
			view := fileTransferView{1, "file_download", string(endpoint.ID), args[1], localPath, result.Size, hex.EncodeToString(result.SHA256)}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tdownloaded\t%d bytes\t%s\n", localPath, view.Size, view.SHA256)
			return nil
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing local file")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileUploadCommand(runtime *fileCommandRuntime) *cobra.Command {
	var overwrite, jsonOutput bool
	command := &cobra.Command{
		Use: "upload ENDPOINT LOCAL [REMOTE]", Short: "Upload a local file and wait for daemon checksum confirmation", Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			remotePath := ""
			if len(args) == 3 {
				remotePath = args[2]
			} else {
				defaults, err := client.PathDefaults(ctx)
				if err != nil {
					return classifyCLIError(err)
				}
				remotePath = pathpkg.Join(defaults.DefaultCWD, filepath.Base(args[1]))
			}
			result, err := uploadEndpointFile(ctx, client, args[1], remotePath, overwrite)
			if err != nil {
				return classifyCLIError(err)
			}
			view := fileTransferView{1, "file_upload", string(endpoint.ID), result.Path, args[1], result.Size, hex.EncodeToString(result.SHA256)}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tuploaded\t%d bytes\t%s\n", result.Path, result.Size, view.SHA256)
			return nil
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing remote file")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileMkdirCommand(runtime *fileCommandRuntime) *cobra.Command {
	var parents, jsonOutput bool
	command := &cobra.Command{
		Use: "mkdir ENDPOINT PATH", Short: "Create a daemon-owned directory", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSingleMutation(cmd, runtime, args[0], jsonOutput, func(ctx context.Context, client *protocol.Client) (*protocol.FileOperationResult, error) {
				return client.FileMkdir(ctx, protocol.FilePathParams{Path: args[1], Recursive: parents})
			})
		},
	}
	command.Flags().BoolVarP(&parents, "parents", "p", false, "create parent directories")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileRenameCommand(runtime *fileCommandRuntime) *cobra.Command {
	var overwrite, jsonOutput bool
	command := &cobra.Command{
		Use: "rename ENDPOINT OLD NEW", Short: "Rename a daemon-owned path", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileSingleMutation(cmd, runtime, args[0], jsonOutput, func(ctx context.Context, client *protocol.Client) (*protocol.FileOperationResult, error) {
				return client.FileRename(ctx, protocol.FileRenameParams{Path: args[1], NewPath: args[2], Overwrite: overwrite})
			})
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing target")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileCopyMoveCommand(runtime *fileCommandRuntime, move bool) *cobra.Command {
	var overwrite, jsonOutput bool
	verb := "copy"
	if move {
		verb = "move"
	}
	command := &cobra.Command{
		Use: verb + " ENDPOINT SRC... DEST_DIR", Short: strings.ToUpper(verb[:1]) + verb[1:] + " daemon-owned paths", Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			params := protocol.FileCopyMoveParams{Paths: append([]string(nil), args[1:len(args)-1]...), TargetDir: args[len(args)-1], Overwrite: overwrite}
			var result *protocol.FileBatchResult
			if move {
				result, err = client.FileMove(ctx, params)
			} else {
				result, err = client.FileCopy(ctx, params)
			}
			if err != nil {
				return classifyCLIError(err)
			}
			return writeFileOperationResults(cmd, endpoint.ID, "file_"+verb, result.Results, jsonOutput)
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace existing targets")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newFileRemoveCommand(runtime *fileCommandRuntime) *cobra.Command {
	var recursive, jsonOutput bool
	command := &cobra.Command{
		Use: "remove ENDPOINT PATH...", Short: "Remove daemon-owned paths", Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := runtime.context(cmd)
			defer cancel()
			client, endpoint, closeClient, err := runtime.open(ctx, cmd, args[0])
			if err != nil {
				return err
			}
			defer closeClient()
			results := make([]protocol.FileOperationResult, 0, len(args)-1)
			for _, path := range args[1:] {
				result, requestErr := client.FileDelete(ctx, protocol.FilePathParams{Path: path, Recursive: recursive})
				if requestErr != nil {
					return classifyCLIError(requestErr)
				}
				results = append(results, *result)
			}
			return writeFileOperationResults(cmd, endpoint.ID, "file_remove", results, jsonOutput)
		},
	}
	command.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove directories recursively")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func runFileSingleMutation(cmd *cobra.Command, runtime *fileCommandRuntime, endpointValue string, jsonOutput bool, operation func(context.Context, *protocol.Client) (*protocol.FileOperationResult, error)) error {
	ctx, cancel := runtime.context(cmd)
	defer cancel()
	client, endpoint, closeClient, err := runtime.open(ctx, cmd, endpointValue)
	if err != nil {
		return err
	}
	defer closeClient()
	result, err := operation(ctx, client)
	if err != nil {
		return classifyCLIError(err)
	}
	return writeFileOperationResults(cmd, endpoint.ID, "file_operation", []protocol.FileOperationResult{*result}, jsonOutput)
}

func writeFileOperationResults(cmd *cobra.Command, endpointID connection.EndpointID, kind string, results []protocol.FileOperationResult, jsonOutput bool) error {
	views := make([]fileOperationView, 0, len(results))
	failures := 0
	for _, result := range results {
		views = append(views, fileOperationView{result.Path, result.TargetPath, result.Success, result.ErrorCode, result.ErrorMessage})
		if !result.Success {
			failures++
		}
	}
	if jsonOutput {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			SchemaVersion int                 `json:"schema_version"`
			Kind          string              `json:"kind"`
			EndpointID    string              `json:"endpoint_id"`
			Results       []fileOperationView `json:"results"`
		}{1, kind, string(endpointID), views}); err != nil {
			return err
		}
	} else {
		for _, view := range views {
			status := "ok"
			if !view.Success {
				status = view.ErrorCode + ": " + view.Error
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", view.Path, view.TargetPath, status)
		}
	}
	if failures > 0 {
		code := 8
		if len(results) == 1 {
			code = 4
		}
		return &cliError{code: code, message: fmt.Sprintf("%d of %d file operations failed", failures, len(results))}
	}
	return nil
}

func downloadEndpointFile(ctx context.Context, client *protocol.Client, remotePath string, writer io.Writer) (protocol.FileTransferResult, error) {
	opened, err := client.FileDownloadOpen(ctx, protocol.FileDownloadOpenParams{Path: remotePath})
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if err := validateFileTransferOpen(opened, -1, false); err != nil {
		cancelFileTransfer(client, opened.TransferID)
		return protocol.FileTransferResult{}, err
	}
	completed := false
	defer func() {
		if !completed {
			cancelFileTransfer(client, opened.TransferID)
		}
	}()
	stream, stop := client.Stream(opened.Channel)
	defer stop()
	hash := sha256.New()
	offset := opened.Offset
	for {
		select {
		case <-ctx.Done():
			return protocol.FileTransferResult{}, ctx.Err()
		case frame, ok := <-stream:
			if !ok {
				return protocol.FileTransferResult{}, io.ErrUnexpectedEOF
			}
			switch frame.Type {
			case wire.TypeFileData:
				data, err := protocol.DecodeFileTransferData(frame.Payload)
				if err != nil || data.Offset != offset {
					return protocol.FileTransferResult{}, fmt.Errorf("invalid download data at offset %d", offset)
				}
				if _, err := writer.Write(data.Data); err != nil {
					return protocol.FileTransferResult{}, err
				}
				_, _ = hash.Write(data.Data)
				offset += int64(len(data.Data))
				ack, err := protocol.EncodeFileTransferAck(protocol.FileTransferAck{Offset: offset, WindowBytes: int64(len(data.Data))})
				if err != nil {
					return protocol.FileTransferResult{}, err
				}
				if err := client.SendFileFrame(opened.Channel, wire.TypeFileAck, ack); err != nil {
					return protocol.FileTransferResult{}, err
				}
			case wire.TypeFileFinish:
				finish, err := protocol.DecodeFileTransferFinish(frame.Payload)
				if err != nil {
					return protocol.FileTransferResult{}, err
				}
				if finish.Size != offset || finish.Size != opened.Size || !bytes.Equal(finish.SHA256, hash.Sum(nil)) {
					return protocol.FileTransferResult{}, fmt.Errorf("download size or SHA-256 mismatch")
				}
				completed = true
				return protocol.FileTransferResult{Path: opened.Path, Size: finish.Size, SHA256: finish.SHA256}, nil
			case wire.TypeError:
				message, decodeErr := protocol.DecodeErrorPayload(frame.Payload)
				if decodeErr != nil {
					return protocol.FileTransferResult{}, decodeErr
				}
				return protocol.FileTransferResult{}, &protocol.RequestError{Code: message.Error.Code, Message: message.Error.Message}
			default:
				return protocol.FileTransferResult{}, fmt.Errorf("unexpected download frame type %d", frame.Type)
			}
		}
	}
}

func downloadEndpointFileAtomic(ctx context.Context, client *protocol.Client, remotePath, localPath string, overwrite bool) (protocol.FileTransferResult, error) {
	localPath = filepath.Clean(strings.TrimSpace(localPath))
	if localPath == "." || localPath == "" {
		return protocol.FileTransferResult{}, usageCLIError("local download path is required")
	}
	if !overwrite {
		if _, err := os.Lstat(localPath); err == nil {
			return protocol.FileTransferResult{}, os.ErrExist
		} else if !os.IsNotExist(err) {
			return protocol.FileTransferResult{}, err
		}
	}
	parent := filepath.Dir(localPath)
	temporary, err := os.CreateTemp(parent, ".termx-download-*")
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return protocol.FileTransferResult{}, err
	}
	result, err := downloadEndpointFile(ctx, client, remotePath, temporary)
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if err := temporary.Sync(); err != nil {
		return protocol.FileTransferResult{}, err
	}
	if err := temporary.Close(); err != nil {
		return protocol.FileTransferResult{}, err
	}
	if overwrite {
		err = os.Rename(temporaryPath, localPath)
	} else {
		err = os.Link(temporaryPath, localPath)
		if err == nil {
			_ = os.Remove(temporaryPath)
		}
	}
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	committed = true
	return result, nil
}

func uploadEndpointFile(ctx context.Context, client *protocol.Client, localPath, remotePath string, overwrite bool) (protocol.FileTransferResult, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if !info.Mode().IsRegular() {
		return protocol.FileTransferResult{}, fmt.Errorf("upload source must be a regular file")
	}
	opened, err := client.FileUploadOpen(ctx, protocol.FileUploadOpenParams{Path: remotePath, Size: info.Size(), Overwrite: overwrite})
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if err := validateFileTransferOpen(opened, info.Size(), false); err != nil {
		cancelFileTransfer(client, opened.TransferID)
		return protocol.FileTransferResult{}, err
	}
	completed := false
	defer func() {
		if !completed {
			cancelFileTransfer(client, opened.TransferID)
		}
	}()
	stream, stop := client.Stream(opened.Channel)
	defer stop()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return protocol.FileTransferResult{}, err
	}
	hash := sha256.New()
	if opened.Offset > 0 {
		if _, err := io.CopyN(hash, file, opened.Offset); err != nil {
			return protocol.FileTransferResult{}, err
		}
	}
	buffer := make([]byte, max(1, opened.ChunkBytes))
	offset := opened.Offset
	for offset < opened.Size {
		remaining := opened.Size - offset
		chunk := buffer
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		n, readErr := io.ReadFull(file, chunk)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return protocol.FileTransferResult{}, readErr
		}
		chunk = chunk[:n]
		_, _ = hash.Write(chunk)
		payload, err := protocol.EncodeFileTransferData(protocol.FileTransferData{Offset: offset, Data: chunk})
		if err != nil {
			return protocol.FileTransferResult{}, err
		}
		if err := client.SendFileFrame(opened.Channel, wire.TypeFileData, payload); err != nil {
			return protocol.FileTransferResult{}, err
		}
		frame, err := nextFileFrame(ctx, stream)
		if err != nil {
			return protocol.FileTransferResult{}, err
		}
		if frame.Type == wire.TypeError {
			message, decodeErr := protocol.DecodeErrorPayload(frame.Payload)
			if decodeErr != nil {
				return protocol.FileTransferResult{}, decodeErr
			}
			return protocol.FileTransferResult{}, &protocol.RequestError{Code: message.Error.Code, Message: message.Error.Message}
		}
		if frame.Type != wire.TypeFileAck {
			return protocol.FileTransferResult{}, fmt.Errorf("unexpected upload frame type %d", frame.Type)
		}
		ack, err := protocol.DecodeFileTransferAck(frame.Payload)
		if err != nil || ack.Offset != offset+int64(n) {
			return protocol.FileTransferResult{}, fmt.Errorf("invalid upload acknowledgement")
		}
		offset = ack.Offset
	}
	digest := hash.Sum(nil)
	finish, err := protocol.EncodeFileTransferFinish(protocol.FileTransferFinish{Size: opened.Size, SHA256: digest})
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if err := client.SendFileFrame(opened.Channel, wire.TypeFileFinish, finish); err != nil {
		return protocol.FileTransferResult{}, err
	}
	frame, err := nextFileFrame(ctx, stream)
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if frame.Type == wire.TypeError {
		message, decodeErr := protocol.DecodeErrorPayload(frame.Payload)
		if decodeErr != nil {
			return protocol.FileTransferResult{}, decodeErr
		}
		return protocol.FileTransferResult{}, &protocol.RequestError{Code: message.Error.Code, Message: message.Error.Message}
	}
	if frame.Type != wire.TypeFileResult {
		return protocol.FileTransferResult{}, fmt.Errorf("unexpected upload completion frame %d", frame.Type)
	}
	result, err := protocol.DecodeFileTransferResult(frame.Payload)
	if err != nil {
		return protocol.FileTransferResult{}, err
	}
	if result.Size != opened.Size || !bytes.Equal(result.SHA256, digest) {
		return protocol.FileTransferResult{}, fmt.Errorf("upload completion size or SHA-256 mismatch")
	}
	completed = true
	return result, nil
}

func validateFileTransferOpen(opened *protocol.FileTransferOpenResult, expectedSize int64, allowResume bool) error {
	if opened == nil || strings.TrimSpace(opened.TransferID) == "" || opened.Channel == 0 || strings.TrimSpace(opened.Path) == "" {
		return fmt.Errorf("daemon returned incomplete file transfer metadata")
	}
	if opened.Size < 0 || (expectedSize >= 0 && opened.Size != expectedSize) {
		return fmt.Errorf("daemon returned an invalid file transfer size")
	}
	if opened.Offset < 0 || opened.Offset > opened.Size || (!allowResume && opened.Offset != 0) {
		return fmt.Errorf("daemon returned an invalid file transfer offset")
	}
	if opened.WindowBytes <= 0 || opened.ChunkBytes <= 0 || opened.ChunkBytes > maxCLIFileChunkBytes {
		return fmt.Errorf("daemon returned invalid file transfer flow-control metadata")
	}
	return nil
}

func cancelFileTransfer(client *protocol.Client, transferID string) {
	if client == nil || strings.TrimSpace(transferID) == "" {
		return
	}
	cancelContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = client.FileTransferCancel(cancelContext, transferID)
}

func nextFileFrame(ctx context.Context, stream <-chan protocol.StreamFrame) (protocol.StreamFrame, error) {
	select {
	case <-ctx.Done():
		return protocol.StreamFrame{}, ctx.Err()
	case frame, ok := <-stream:
		if !ok {
			return protocol.StreamFrame{}, io.ErrUnexpectedEOF
		}
		return frame, nil
	}
}

func fileEntryProjection(entry protocol.FileEntry) fileEntryView {
	return fileEntryView{entry.Path, entry.Name, entry.Type, entry.Size, entry.Mode, formatTerminalTime(entry.ModifiedAt), entry.LinkTarget}
}
