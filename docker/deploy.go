package docker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-isatty"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/flyctl"
	"github.com/superfly/flyctl/terminal"
	"golang.org/x/net/context"
)

type DeployOperation struct {
	ctx             context.Context
	dockerClient    *DockerClient
	apiClient       *api.Client
	dockerAvailable bool
	out             io.Writer
	appName         string
	appConfig       *flyctl.AppConfig
	squash          bool
}

func NewDeployOperation(ctx context.Context, appName string, appConfig *flyctl.AppConfig, apiClient *api.Client, out io.Writer, squash bool) (*DeployOperation, error) {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return nil, err
	}

	op := &DeployOperation{
		ctx:          ctx,
		dockerClient: dockerClient,
		apiClient:    apiClient,
		out:          out,
		appName:      appName,
		appConfig:    appConfig,
		squash:       squash,
	}

	op.dockerAvailable = op.dockerClient.Check(ctx) == nil

	return op, nil
}

func (op *DeployOperation) AppName() string {
	if op.appName != "" {
		return op.appName
	}
	return op.appConfig.AppName
}

func (op *DeployOperation) DockerAvailable() bool {
	return op.dockerAvailable
}

func (op *DeployOperation) DeployImage(imageRef string) (*api.Release, error) {
	//if op.dockerAvailable {
	//	return op.deployImageWithDocker(imageRef)
	//}
	return op.deployImageWithoutDocker(imageRef)
}

func (op *DeployOperation) ValidateConfig() (*api.AppConfig, error) {
	if op.appConfig == nil {
		op.appConfig = flyctl.NewAppConfig()
	}

	printHeader("Validating app configuration")

	parsedConfig, err := op.apiClient.ParseConfig(op.appName, op.appConfig.Definition)
	if err != nil {
		return nil, err
	}

	if !parsedConfig.Valid {
		return nil, errors.New("App configuration is not valid")
	}

	op.appConfig.Definition = parsedConfig.Definition

	fmt.Println("-->", "done")

	return parsedConfig, nil
}

func (op *DeployOperation) deployImageWithDocker(imageRef string) (*api.Release, error) {
	deploymentTag, err := op.resolveAndTagImageRef(imageRef)
	if err != nil {
		return nil, err
	}

	if err := op.pushImage(deploymentTag); err != nil {
		return nil, err
	}

	if err := op.optimizeImage(deploymentTag); err != nil {
		return nil, err
	}

	release, err := op.deployImage(deploymentTag)
	if err != nil {
		return nil, err
	}

	op.cleanDeploymentTags()

	return release, nil

}

func (op *DeployOperation) deployImageWithoutDocker(imageRef string) (*api.Release, error) {
	ref, err := checkManifest(op.ctx, imageRef, "")
	if err != nil {
		return nil, err
	}

	if err := op.optimizeImage(ref.Remote()); err != nil {
		return nil, err
	}

	return op.deployImage(ref.Remote())
}

func (op *DeployOperation) resolveAndTagImageRef(imageRef string) (string, error) {
	printHeader("Resolving image")

	img, err := op.dockerClient.ResolveImage(op.ctx, imageRef)
	if err != nil {
		return "", err
	}

	if img == nil {
		return "", fmt.Errorf("Could not resolve image %s", imageRef)
	}

	fmt.Println("-->", img.ID)

	imageTag := newDeploymentTag(op.appConfig.AppName)

	printHeader("Creating deployment tag")
	if err := op.dockerClient.TagImage(op.ctx, img.ID, imageTag); err != nil {
		return "", err
	}
	fmt.Println("-->", imageTag)

	return imageTag, nil
}

func (op *DeployOperation) pushImage(imageTag string) error {
	printHeader("Pushing image")

	if imageTag == "" {
		return errors.New("invalid image reference")
	}

	if err := op.dockerClient.PushImage(op.ctx, imageTag, op.out); err != nil {
		return err
	}
	fmt.Println("-->", "done")

	return nil
}

func (op *DeployOperation) optimizeImage(imageTag string) error {
	printHeader("Optimizing image")
	defer fmt.Println("-->", "done")

	if isatty.IsTerminal(os.Stdout.Fd()) {
		s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
		s.Writer = os.Stderr
		s.Prefix = "building fs... "
		s.Start()
		defer s.Stop()
	}

	delay := 0 * time.Second

	for {
		select {
		case <-time.After(delay):
			status, err := op.apiClient.OptimizeImage(op.AppName(), imageTag)
			if err != nil {
				return err
			}
			if status != "in_progress" {
				return nil
			}
			delay = 1 * time.Second
		case <-op.ctx.Done():
			return op.ctx.Err()
		}
	}
}

func (op *DeployOperation) deployImage(imageTag string) (*api.Release, error) {
	input := api.DeployImageInput{AppID: op.AppName(), Image: imageTag}

	if op.appConfig != nil && len(op.appConfig.Definition) > 0 {
		x := api.Definition(op.appConfig.Definition)
		input.Definition = &x
	}

	printHeader("Creating Release")
	release, err := op.apiClient.DeployImage(input)
	if err != nil {
		return nil, err
	}
	return release, err
}

func (op *DeployOperation) cleanDeploymentTags() {
	err := op.dockerClient.DeleteDeploymentImages(op.ctx, op.AppName())
	if err != nil {
		terminal.Debug("Error cleaning deployment tags", err)
	}
}

func printHeader(message string) {
	fmt.Println(aurora.Blue("==>"), message)
}
