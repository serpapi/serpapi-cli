package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

var imageCmd = &cobra.Command{
	Use:     "image <file>",
	Aliases: []string{"upload-image"},
	Short:   "Upload an image for a Google Lens search",
	Long: `Upload an image with the SerpApi Image API and print the resulting image_id.

The image_id works only with engine=google_lens, for 10 minutes. Supported
formats are JPG/JPEG, PNG and WebP up to 500 KB.

An upload doesn't count against your searches, but it needs searches left this
month and this hour, and uploads have their own hourly limit:
account_rate_limit_per_hour in "serpapi account".

To upload and search Google Lens in one step, use
"serpapi search engine=google_lens --image <file>" instead.

Pass "-" as the file to read the image from standard input.`,
	Example: `  serpapi image ./photo.jpg
  serpapi image ./photo.jpg --jq .image_id
  curl -s https://example.com/photo.jpg | serpapi image -`,
	Args: cobra.ExactArgs(1),
	RunE: runImage,
}

func init() {
	rootCmd.AddCommand(imageCmd)
}

func runImage(cmd *cobra.Command, args []string) error {
	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}

	result, err := uploadImage(cmd, newClient(apiKey), args[0])
	if err != nil {
		return err
	}
	return handleOutput(cmd, result, nil)
}

// uploadImage uploads the image at path, or from stdin when path is "-",
// showing a spinner while the request is in flight.
func uploadImage(cmd *cobra.Command, client *api.Client, path string) (json.RawMessage, error) {
	ctx := cmd.Context()
	var content []byte
	if path == "-" {
		in := cmd.InOrStdin()
		var err error
		content, err = readInterruptibly(ctx, func() ([]byte, error) { return api.ReadImage(in) },
			"Stopped while reading the image from stdin; nothing was uploaded")
		if isInterrupted(err) {
			return nil, err
		}
		if err != nil {
			return nil, &clierrors.UsageError{Message: "Cannot read image from stdin: " + err.Error()}
		}
	}

	return withSpinner("Uploading image...", func() (json.RawMessage, error) {
		if content != nil {
			return client.UploadImageBytes(ctx, content, "")
		}
		return client.UploadImage(ctx, path)
	})
}

// uploadImageID uploads an image and returns the image_id from the response.
func uploadImageID(cmd *cobra.Command, client *api.Client, path string) (string, error) {
	raw, err := uploadImage(cmd, client, path)
	if err != nil {
		return "", err
	}
	var upload struct {
		ImageID string `json:"image_id"`
	}
	if err := json.Unmarshal(raw, &upload); err != nil || upload.ImageID == "" {
		return "", &clierrors.APIError{Message: "Image upload response did not include an image_id"}
	}
	return upload.ImageID, nil
}
