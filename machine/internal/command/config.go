package command

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"ai-control-mesh/machine/internal/settings"
)

var featureDescriptions = []string{
	"Launch coding agents here as your user, in allowed project folders.",
	"Open remote terminals as your user. The starting folder is restricted; shell commands can access anything your user can.",
	"Browse and read files in allowed project folders, including hidden files. File contents are sent to the server.",
	"Let the machine owner or an admin replace agent-machine and agent with the server's verified build.",
	"Manage local model servers: install llama.cpp, start and stop servers, change settings, download and delete models.",
	"Run supported model-server containers with Docker or Podman. Docker access can grant root privileges on this computer.",
}

func configCommand(args []string, input io.Reader, out io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(out, "Usage: agent-machine config\nChoose multiple machine functions by number or name. Enter keeps current settings; none switches every function off.")
		return 0
	}
	if len(args) != 0 {
		return fail("config takes no arguments (use --help)")
	}
	machine := settings.LoadMachine()
	fmt.Fprintln(out, "Choose what this machine allows through AI Control Mesh.")
	fmt.Fprintln(out, "These permissions use programs installed here; selecting them does not install model servers or a container runtime.")
	for i, name := range settings.Toggles {
		state := "off"
		if machine.Toggle(name) {
			state = "on"
		}
		fmt.Fprintf(out, "  %d. %s [%s]\n     %s\n", i+1, name, state, featureDescriptions[i])
	}
	fmt.Fprintln(out, "Launches, file viewing and terminal starting folders use roots set with: agent-machine roots add <folder>")
	fmt.Fprintln(out, "Server permissions and sharing rules still apply. Changes reach the server within a minute.")
	fmt.Fprintln(out, "A selection enables only the listed functions and switches all others off.")
	reader := bufio.NewReader(input)
	for {
		fmt.Fprint(out, "Enable functions (comma-separated numbers or names, e.g. 1,3; none = all off; Enter = keep current): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintln(out, "\nConfiguration cancelled; permissions unchanged. Run agent-machine config from a terminal to choose functions.")
			return 1
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			fmt.Fprintln(out, "Keeping current machine permissions.")
			return 0
		}
		var names []string
		if line != "none" {
			parts := strings.Split(line, ",")
			for i, part := range parts {
				if number, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && number >= 1 && number <= len(settings.Toggles) {
					parts[i] = settings.Toggles[number-1]
				}
			}
			names, err = settings.ParseToggles(strings.Join(parts, ","))
			if err != nil || len(names) == 0 {
				fmt.Fprintln(out, "Choose listed numbers or names, or none. No permissions have changed.")
				continue
			}
		}
		// Save only after a complete valid selection. Preserve identity, roots and other settings.
		machine = settings.LoadMachine()
		for _, name := range settings.Toggles {
			machine.SetToggle(name, contains(names, name))
		}
		if err := machine.Save(); err != nil {
			fmt.Fprintf(out, "Couldn't save machine permissions: %v\n", err)
			return 1
		}
		label := strings.Join(names, ", ")
		if len(names) == 0 {
			label = "none"
		}
		fmt.Fprintf(out, "Enabled: %s. All other machine functions are off.\n", label)
		for _, name := range names {
			for _, note := range machine.ToggleNotes(name) {
				fmt.Fprintln(out, note)
			}
		}
		fmt.Fprintln(out, "Change these choices later with: agent-machine config")
		return 0
	}
}
