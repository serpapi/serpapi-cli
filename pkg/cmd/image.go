package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/serpapi/serpapi-cli/pkg/api"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

var imageCmd = &cobra.Command{
	Use:     "image <file>",
	Aliases: []string{"upload-image"},
	Short:   "Upload an image for use with image search engines like Google Lens",
	Long: `Upload an image with the SerpApi Image API and print the resulting image_id.

The image_id can be passed to Search API engines that accept uploaded images,
such as google_lens. Supported formats are JPG/JPEG, PNG and WebP up to 500 KB.
Uploaded image IDs expire after 10 minutes.

To upload and search in a single step, use "serpapi search --image <file>" instead.

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

	result, err := uploadImage(cmd.Context(), api.New(apiKey), args[0])
	if err != nil {
		return err
	}
	return handleOutput(result)
}

// uploadImage uploads the image at path, or from stdin when path is "-",
// showing a spinner while the request is in flight.
func uploadImage(ctx context.Context, client *api.Client, path string) (json.RawMessage, error) {
	var content []byte
	if path == "-" {
		var err error
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, &clierrors.UsageError{Message: "Cannot read image from stdin: " + err.Error()}
		}
	}

	sp := newSpinner("Uploading image...")
	sp.Start()
	defer sp.Stop()
	if content != nil {
		return client.UploadImageBytes(ctx, content, "")
	}
	return client.UploadImage(ctx, path)
}

// uploadImageID uploads an image and returns the image_id from the response.
func uploadImageID(ctx context.Context, client *api.Client, path string) (string, error) {
	raw, err := uploadImage(ctx, client, path)
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
