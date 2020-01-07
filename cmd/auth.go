package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/briandowns/spinner"
	"github.com/logrusorgru/aurora"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/src/flyctl"
	"github.com/superfly/flyctl/terminal"
)

func newAuthCommand() *Command {
	cmd := &Command{
		Command: &cobra.Command{
			Use:   "auth",
			Short: "manage authentication",
			Long:  "Authenticate with Fly (and logout if you need to). Start with the \"login\" subcommand.",
		},
	}

	BuildCommand(cmd, runWhoami, "whoami", "show the currently authenticated user", os.Stdout, true)
	BuildCommand(cmd, runAuthToken, "token", "show the current auth token", os.Stdout, true)

	login := BuildCommand(cmd, runLogin, "login", "log in a user", os.Stdout, false)
	login.AddBoolFlag(BoolFlagOpts{
		Name:        "interactive",
		Shorthand:   "i",
		Description: "log in with an email and password interactively",
	})
	login.AddStringFlag(StringFlagOpts{
		Name:        "email",
		Description: "login email",
	})
	login.AddStringFlag(StringFlagOpts{
		Name:        "password",
		Description: "login password",
	})
	login.AddStringFlag(StringFlagOpts{
		Name:        "otp",
		Description: "one time password",
	})

	BuildCommand(cmd, runLogout, "logout", "log out the user", os.Stdout, true)

	return cmd
}

func runWhoami(ctx *CmdContext) error {
	user, err := ctx.FlyClient.GetCurrentUser()
	if err != nil {
		return err
	}
	fmt.Printf("Current user: %s\n", user.Email)
	return nil
}

func runLogin(ctx *CmdContext) error {
	if ctx.Config.GetBool("interactive") {
		return runInteractiveLogin(ctx)
	}
	if val, _ := ctx.Config.GetString("email"); val != "" {
		return runInteractiveLogin(ctx)
	}
	if val, _ := ctx.Config.GetString("password"); val != "" {
		return runInteractiveLogin(ctx)
	}
	if val, _ := ctx.Config.GetString("otp"); val != "" {
		return runInteractiveLogin(ctx)
	}

	return runWebLogin(ctx)
}

func runWebLogin(ctx *CmdContext) error {
	name, _ := os.Hostname()

	cliAuth, err := api.StartCLISessionWebAuth(name)
	if err != nil {
		return err
	}

	fmt.Println("Opening browser to url", aurora.Bold(cliAuth.AuthURL))

	if err := open.Run(cliAuth.AuthURL); err != nil {
		terminal.Error("Error opening browser. Copy the above url into a browser and continue")
	}

	select {
	case <-time.After(15 * time.Minute):
		return errors.New("Login expired, please try again")
	case cliAuth = <-waitForCLISession(cliAuth.ID):
	}

	if cliAuth.AccessToken == "" {
		return errors.New("Unable to log in, please try again")
	}

	viper.Set(flyctl.ConfigAPIToken, cliAuth.AccessToken)
	if err := flyctl.SaveConfig(); err != nil {
		return err
	}

	if err := ctx.InitApiClient(); err != nil {
		return err
	}

	user, err := ctx.FlyClient.GetCurrentUser()
	if err != nil {
		return err
	}

	fmt.Println("Successfully logged in as", aurora.Bold(user.Email))

	return nil
}

func waitForCLISession(id string) <-chan api.CLISessionAuth {
	done := make(chan api.CLISessionAuth, 0)

	go func() {
		s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
		s.Writer = os.Stderr
		s.Prefix = "Waiting for session..."
		s.FinalMSG = "Waiting for session...Done\n"
		s.Start()
		defer s.Stop()

		for {
			time.Sleep(1 * time.Second)
			cliAuth, _ := api.GetAccessTokenForCLISession(id)

			if cliAuth.AccessToken != "" {
				done <- cliAuth
				break
			}
		}
	}()

	return done
}

func runInteractiveLogin(ctx *CmdContext) error {
	email, _ := ctx.Config.GetString("email")
	if email == "" {
		prompt := &survey.Input{
			Message: "Email:",
		}
		if err := survey.AskOne(prompt, &email, survey.WithValidator(survey.Required)); err != nil {
			if isInterrupt(err) {
				return nil
			}
		}
	}

	password, _ := ctx.Config.GetString("password")
	if password == "" {
		prompt := &survey.Password{
			Message: "Password:",
		}
		if err := survey.AskOne(prompt, &password, survey.WithValidator(survey.Required)); err != nil {
			if isInterrupt(err) {
				return nil
			}
		}
	}

	otp, _ := ctx.Config.GetString("otp")
	if otp == "" {
		prompt := &survey.Password{
			Message: "One Time Password (if any):",
		}
		if err := survey.AskOne(prompt, &otp); err != nil {
			if isInterrupt(err) {
				return nil
			}
		}
	}

	accessToken, err := api.GetAccessToken(email, password, otp)

	if err != nil {
		return err
	}

	viper.Set(flyctl.ConfigAPIToken, accessToken)

	return flyctl.SaveConfig()
}

func runLogout(ctx *CmdContext) error {
	viper.Set(flyctl.ConfigAPIToken, "")

	if err := flyctl.SaveConfig(); err != nil {
		return err
	}

	fmt.Println("Session removed")

	return nil
}

func runAuthToken(ctx *CmdContext) error {
	token, _ := ctx.GlobalConfig.GetString(flyctl.ConfigAPIToken)

	fmt.Println(token)

	return nil
}
