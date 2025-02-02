package cli

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	internalv1 "github.com/acorn-io/runtime/pkg/apis/internal.acorn.io/v1"
	cli "github.com/acorn-io/runtime/pkg/cli/builder"
	"github.com/acorn-io/runtime/pkg/client"
	acornsign "github.com/acorn-io/runtime/pkg/cosign"
	signatureannotations "github.com/acorn-io/runtime/pkg/imageselector/signatures/annotations"
	"github.com/acorn-io/runtime/pkg/tags"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/pterm/pterm"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	"github.com/sigstore/cosign/v2/pkg/signature"
	sigsig "github.com/sigstore/sigstore/pkg/signature"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/acorn-io/runtime/pkg/prompt"
)

func NewImageSign(c CommandContext) *cobra.Command {
	cmd := cli.Command(&ImageSign{client: c.ClientFactory}, cobra.Command{
		Use:               "sign IMAGE_NAME [flags]",
		Example:           `acorn image sign my-image --key ./my-key`,
		SilenceUsage:      true,
		Short:             "Sign an Image",
		ValidArgsFunction: newCompletion(c.ClientFactory, imagesCompletion(true)).complete,
		Args:              cobra.ExactArgs(1),
		Hidden:            true,
	})
	_ = cmd.MarkFlagFilename("key")
	return cmd
}

type ImageSign struct {
	client      ClientFactory
	Key         string            `usage:"Key to use for signing" short:"k" local:"true"`
	Annotations map[string]string `usage:"Annotations to add to the signature" short:"a" local:"true" name:"annotation"`
}

func (a *ImageSign) Run(cmd *cobra.Command, args []string) error {
	if a.Key == "" {
		return fmt.Errorf("key is required")
	}

	// Validate user-provided Annotations
	_, err := signatureannotations.GenerateSelector(internalv1.SignatureAnnotations{Match: a.Annotations}, signatureannotations.LabelSelectorOpts{LabelRequirementErrorFilters: []utilerrors.Matcher{signatureannotations.IgnoreInvalidFieldErrors(signatureannotations.LabelValueMaxLengthErrMsg, signatureannotations.LabelValueRegexpErrMsg)}})
	if err != nil {
		return fmt.Errorf("failed to parse provided annotations: %w", err)
	}

	imageName := args[0]

	c, err := a.client.CreateDefault()
	if err != nil {
		return err
	}

	auth, err := getAuthForImage(cmd.Context(), a.client, imageName)
	if err != nil {
		return err
	}

	// not failing here, since it could be a local image
	ref, _ := name.ParseReference(imageName)

	details, err := c.ImageDetails(cmd.Context(), args[0], &client.ImageDetailsOptions{
		Auth: auth,
	})
	if err != nil {
		return err
	}

	targetDigest := ref.Context().Digest(details.AppImage.Digest)

	pterm.Info.Printf("Signing Image %s (digest: %s)\n", imageName, targetDigest)

	pass, err := getPrivateKeyPass()
	if err != nil {
		return err
	}
	if len(pass) == 0 {
		pass = nil // nothing instead of empty pass
	}

	pf := func(_ bool) ([]byte, error) {
		return pass, nil
	}

	// Get a sigSigner-verifier from a private key and if the key type is not supported, try to import it first
	var sigSigner sigsig.SignerVerifier

	if len(a.Key) > 255 || strings.Contains(strings.Trim(a.Key, "\n"), "\n") {
		// Not a file (filename too long or contains newlines) - load from raw key data
		sigSigner, err = cosign.LoadPrivateKey([]byte(a.Key), pass)
	} else {
		var finfo os.FileInfo
		finfo, err = os.Stat(a.Key)
		if err != nil {
			if os.IsNotExist(err) || strings.Contains("\n", a.Key) {
				// Not a file - load from raw key data
				sigSigner, err = cosign.LoadPrivateKey([]byte(a.Key), pass)
			} else {
				return fmt.Errorf("failed to stat key file: %w", err)
			}
		} else {
			if finfo.IsDir() {
				return fmt.Errorf("invalid key file: is directory")
			}
			// Load from file
			sigSigner, err = signature.SignerVerifierFromKeyRef(cmd.Context(), a.Key, pf)
		}
	}

	if err != nil {
		if !strings.Contains(err.Error(), "unsupported pem type") {
			return fmt.Errorf("failed to create signer from private key: %w", err)
		}
		logrus.Debugf("Key %s is not a supported PEM key, importing...\n", a.Key)
		keyBytes, err := acornsign.ImportKeyPair(a.Key, pass)
		if err != nil {
			return fmt.Errorf("failed to import private key: %w", err)
		}
		sigSigner, err = cosign.LoadPrivateKey(keyBytes.PrivateBytes, keyBytes.Password())
		if err != nil {
			return fmt.Errorf("failed to create signer from imported private key: %w", err)
		}
	}

	signedName := ref.String()
	if tags.IsLocalReference(signedName) {
		// If we called it by ID(-Prefix), we're signing with the fully resolved ID
		signedName = details.AppImage.ID
	}
	annotations := acornsign.GetDefaultSignatureAnnotations(signedName)
	if a.Annotations != nil {
		for k, v := range a.Annotations {
			annotations[k] = v
		}
	}

	payload, signature, err := sigsig.SignImage(sigSigner, targetDigest, annotations)
	if err != nil {
		return err
	}

	logrus.Debugf("Payload Annotations: %#v", annotations)

	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	imageSignOpts := &client.ImageSignOptions{
		Auth: auth,
	}

	pubkey, err := sigSigner.PublicKey()
	if err != nil {
		return err
	}

	if pubkey != nil {
		pem, _, err := acornsign.PemEncodeCryptoPublicKey(pubkey)
		if err != nil {
			return err
		}

		imageSignOpts.PublicKey = string(pem)
	}

	sig, err := c.ImageSign(cmd.Context(), imageName, payload, signatureB64, imageSignOpts)
	if err != nil {
		return err
	}

	pterm.Success.Printf("Created signature %s\n", sig.SignatureDigest)

	return nil
}

// Get password for private key from environment, prompt or stdin (piped)
// Adapted from Cosign's readPasswordFn
func getPrivateKeyPass() ([]byte, error) {
	pw, ok := os.LookupEnv("ACORN_IMAGE_SIGN_PASSWORD")
	switch {
	case ok:
		return []byte(pw), nil
	case isTerm():
		return prompt.Password("Enter password for private key:")
	default:
		return io.ReadAll(os.Stdin)
	}
}

func isTerm() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) != 0
}
