// Copyright 2020 Mohammed El Bahja. All rights reserved.
// Use of this source code is governed by a MIT license.

package actions

import (
	"fmt"
	"io/ioutil"
	"os"
	"log"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/guark/guark"
	"github.com/guark/guark/app/utils"
	"github.com/guark/guark/cmd/guark/stdio"
	"github.com/guark/guark/internal/generator"
	"github.com/manifoldco/promptui"
	"github.com/otiai10/copy"
	"github.com/urfave/cli/v2"
	"github.com/zserge/webview"
)

var (
	supportedOses = []string{"linux", "darwin", "windows"}
	BuildFlags    = []cli.Flag{
		&cli.StringFlag{
			Name:  "pkg",
			Usage: "Set your package manager.",
			Value: "yarn",
		},
		&cli.StringSliceFlag{
			Name:  "target",
			Usage: "Set build targets",
			Value: cli.NewStringSlice(getHost()),
		},
		&cli.StringFlag{
			Name:  "dest",
			Usage: "Build to a specific destination.",
			Value: "dist",
		},
		&cli.BoolFlag{
			Name:  "rm",
			Usage: "Remove dest path if exists.",
		},
	}
)


func Build(c *cli.Context) (err error) {

	var (
		tmp     string
		b       = build{}
		out     = stdio.NewWriter()
		targets = c.StringSlice("target")
	)

	out.Done("Start building...")

	if utils.IsDir(c.String("dest")) {

		if c.Bool("rm") {

		} else if err = deleteDest(c.String("dest")); err != nil {
			return
		}

		os.RemoveAll(c.String("dest"))
	}

	if err = os.MkdirAll(c.String("dest"), 0754); err != nil {
		return
	}

	for i := range targets {

		if err = checkTarget(targets[i]); err != nil {
			out.Err("Invalid target name.")
			return
		}
	}

	if err = guark.UnmarshalGuarkFile("guark.yaml", &b); err != nil {
		return
	}

	out.Done("Guark build initialized ⚙️")

	tmp, err = ioutil.TempDir("", "guark")

	if err != nil {
		return
	}

	// Clear tmp.
	// defer os.RemoveAll(tmp)

	out.Update("Building app ui.")

	// Build ui
	o, err := b.ui(c.String("pkg"), tmp)

	if err != nil {

		fmt.Println(string(o))
		return
	}

	out.Done("Guark UI builded 🙈")

	if err = b.assets(tmp); err != nil {
		return
	}

	out.Done("Guark UI assets indexed 🙉")

	if err = b.embed([]string{"guark.yaml"}, tmp); err != nil {
		return
	}

	for i := range targets {

		if err = b.target(targets[i], tmp); err != nil {
			return
		}

		if err = b.bundle(targets[i], tmp, c.String("dest")); err != nil {
			return
		}

		out.Done(fmt.Sprintf("Guark build for %s 🙊", targets[i]))
	}

	// if c.String("dest") == "" {
	// 	// move things.
	// }

	// out.Done("Guark build finished 🚀🚀")

	// time.Sleep(100000 * time.Second)

	return
}

func deleteDest(dest string) error {

	prompt := promptui.Prompt{
		Label:     fmt.Sprintf("confirm deleting: %s", dest),
		IsConfirm: true,
		Validate: func(v string) error {

			if v == "y" {
				return fmt.Errorf("Are you sure? type uppercase Y.")
			}

			return nil
		},
		Templates: &promptui.PromptTemplates{
			Success: `{{ green "✔"}} {{ cyan "Delete existing dest:" }} `,
		},
	}

	if yes, err := prompt.Run(); yes != "Y" || err != nil {
		return fmt.Errorf("aborted")
	}

	return nil
}


type build struct {
	Version   string `yaml:"guark"`
	ID        string `yaml:"id"`
	LogLevel  string `yaml:"logLevel"`
	LogOutput string `yaml:"logOutput"`
}

func (b build) target(goos string, dir string) error {

	buildDir, err := getBuildDir(goos, dir)

	if err != nil {
		return err
	}

	flags, env, out := getBuildFlagsAndEnvFor(goos, buildDir, b.ID)

	cmd := exec.Command("go", append(flags, "-o", out)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func (b build) ui(pkg string, dir string) ([]byte, error) {

	cmd := exec.Command(pkg, "build")
	cmd.Dir = path("ui")
	cmd.Env = append(os.Environ(), fmt.Sprintf("GUARK_BUILD_DIR=%s/ui", dir))

	return cmd.CombinedOutput()
}

func (b build) assets(dir string) error {


	staticDir := filepath.Join(dir, "assets")

	if err := os.Mkdir(staticDir, 0754); err != nil {
		return err
	}

	return generator.Assets(filepath.Join(dir, "ui"), staticDir, path("lib", "assets.go"), "lib", filepath.Join(dir, "ui"))
}

func (b build) embed(files []string, dir string) error {

	return generator.Embed(files, path("lib", "embed.go"), "lib", filepath.Join(dir, "ui"))
}


func (b build) bundle(osbuild string, src string, dest string) error {

	switch osbuild {
	case "linux":
		return b.bundleLinux(src, dest);
	}

	return nil
}

func (b build) bundleLinux(src string, dest string) error {

	prefix := filepath.Join(dest, "linux")
	assets := filepath.Join(prefix, "datadir", "assets")
	must(os.MkdirAll(assets, 0740))
	must(copy.Copy(filepath.Join(src, "assets"), assets))
	must(copy.Copy(filepath.Join(src, "linux", b.ID), filepath.Join(prefix, "bin", b.ID)))
	must(copy.Copy(filepath.Join(wdir, "res", "icons"), filepath.Join(prefix, "datadir", "icons")))

	make, err := os.Create(filepath.Join(prefix, "Makefile"))

	if err != nil {
		return err
	}

	make.WriteString(fmt.Sprintf(`
# to install the app run: sudo make install
install:
	install -C bin/%[1]s /usr/bin/%[1]s
	mkdir -p /usr/share/%[1]s
	cp -r datadir/ /usr/share/%[1]s
	find /usr/share/%[1]s/ -type d -exec chmod 755 {} \;
	find /usr/share/%[1]s/ -type f -exec chmod 644 {} \;

`, b.ID))

	// TODO: parse .desktop file.

	return nil
}

func getDlls() string {
	return filepath.Join(os.Getenv("GOPATH"), "src", pkgPath(webview.New(true)), "dll")
}

// this function code was stolen from:
// https://stackoverflow.com/a/60846213/5834438
func pkgPath(v interface{}) string {
	if v == nil {
		return ""
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		return val.Elem().Type().PkgPath()
	}
	return val.Type().PkgPath()
}

func getBuildDir(target string, dir string) (string, error) {

	buildDir := filepath.Join(dir, target)

	if err := os.Mkdir(buildDir, 0754); err != nil {
		return "", err
	}

	return buildDir, nil
}

func checkTarget(target string) error {

	for i := range supportedOses {
		if supportedOses[i] == target {
			return nil
		}
	}

	return fmt.Errorf("target: %s not supported yet!", target)
}



func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

