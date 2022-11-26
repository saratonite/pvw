// pvw - by Ally Ring

package main

import (
	"fmt"
	"runtime"
	"golang.org/x/exp/slices"


// All the Charm modules we need
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	// For handling CLI flags (CLI switches) (standard flag module isn't POSIX compliant)
	"github.com/spf13/pflag"

	// For running commands and exiting
	"os"
	"os/exec"

	// For formatting output & parsing input
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------------------------------------------------

// Important Structs

// A process. Contains a PID used to terminate the process later, the name of the executable responsible for that process,
// an array of the ports it uses, and the username of the user that created that process
type process struct {
	id int
	name string
	directory string
	connections []connection
	username string
}

// A connection. Contains a protocol type (typically tcp or udp), connection status, remote address and port,
// and local address and port.
type connection struct {
	protocol string
	status string

	remotePort string
	remoteAddress string

	localPort string
	localAddress string
}

// The settings struct. Contains all the settings for parsing and rendering the table
type settings struct {
	readOnly   bool           	// Allow process termination
	showClosed bool           	// Allow closed ports to be displayed
	listenOnly bool           	// Filter to ports that are listening
	getCwd 	   bool			  	// Enable getting the CWD of a process
	columns    []table.Column 	// The columns that have been selected for rendering
	portFilter []string			// The port numbers to filter by - don't filter if empty
	nameFilter []string			// The port names to filter by - don't filter if empty
}

// ---------------------------------------------------------------------------------------------------------------------

// MODEL
// The bubbletea model, where most of the processed information is stored, ready to be rendered.

type model struct {
	table     	table.Model    	// The table that gets rendered
	rowStarts	[]int			// The end of each process's list of open ports
	processes 	[]process      	// A slice of process structs
	err       	error          	// The most recent error

	// Settings are stored in the settings struct. Includes render and parsing settings
	settings 	settings

	// Used in help menu
	keys       	keyMap 			// The keymap used
	help       	help.Model		// The help bubble that gets rendered
	inputStyle 	lipgloss.Style	// The style used when rendering everything

	// TODO: Allow user to create custom styles? This might be better as a separate module/tool
	// (if it doesn't exist yet).

}

// ---------------------------------------------------------------------------------------------------------------------

// MESSAGES
// Definitions for messages that get sent to bubbletea after processing I/O with tea.cmd

// Util function to get the error text from an errMsg element
func (e errMsg) Error() string { return e.err.Error() }

type processesMsg struct{ 			// A struct comprised of process structs and table rows
	processes []process
	rows []table.Row
	ends []int
}
type errMsg struct{ err error }		// An error message.
type terminateMsg struct{} 			// The message returned when terminating a process doesn't error. This then results
									// in another command being issued to get the latest slice of processes, which
									// should have the terminated process removed if it was successful.

// ---------------------------------------------------------------------------------------------------------------------

// Keybindings. Includes all the keys that can be pressed.

type keyMap struct {
	Up      key.Binding
	Down    key.Binding

	Terminate    key.Binding
	Refresh key.Binding

	Help    key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Terminate: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "terminate selected process"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh the list of processes"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

// ---------------------------------------------------------------------------------------------------------------------

// Help functions. Used in creating the help menu

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Refresh, k.Help},
		{k.Terminate, k.Quit},
	}
}

// ---------------------------------------------------------------------------------------------------------------------

// Lipgloss' base style. Just using the default for now.

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

// ---------------------------------------------------------------------------------------------------------------------

// LSOF Processing
// All the functions and Cmds relating to getting the processes with ports open on macOS and Linux
// TODO: Windows implementation?

// checkProcesses() is the primary function that returns a bubbletea message. It handles all the other functions,
// processes their outputs, then passes those outputs to other functions.
// It takes an input of the render and parsing settings, so that all the parsing and conversion from process structs to
// strings is done inside a goroutine.
func checkProcesses(settingsInfo settings) tea.Cmd {
	return func() tea.Msg {

		out, err := getLsof()


		if err != nil {
			if !(err.Error() == "1") {

				return processesMsg{nil, nil, nil} // No processes, so return empty process slice
			}
			// Error if we fail, rather than running extra code
			return errMsg{err}
		}

		// We have a string that represents the `lsof` output. Parse that
		// into a slice of process structs with the parseLsof() function
		parsed, err := parseLsof(out, settingsInfo)

		if err != nil {
			return errMsg{err}
		}

		formatted, ends, err := formatLsof(parsed,settingsInfo)

		return processesMsg{parsed, formatted, ends}

	}
}

// getLsof() runs the desired command and returns the output as a raw string or an error
func getLsof() (string, error) {
	// Set the command to use and get the output of that command (as well as any error codes we may encounter)
	// Command is `lsof -i -Pn -F cPnpLT`
	cmd := exec.Command("lsof", "-i", "-Pn", "-F", "cPnpLT")
	out, err := cmd.Output()

	// If the error code is 1, then there are no processes with open ports.
	// Assume anything else is an actual error
	if err != nil {
		// There's an error, return an empty string & the error. We'll parse error code 1 (no processes found) later on.
		return "", err
	}

	// We have valid data, so return it! The out variable is a byte array, so convert it to a string first.
	return string(out), err

}

// getCwd() gets the working directory of a process from a PID
func getCwd(pid int) (string, error) {
	pidString := strconv.Itoa(pid)

	// Gets the process' open files, including current working directory.
	// Command is `lsof -p PID -F n`
	cmd := exec.Command("lsof", "-p", pidString, "-F", "n")
	out, err := cmd.Output()

	if err != nil {
		return "", err
	}

	// The field can be identified with the string 'fcwd\nn', then the next section until a newline
	// So split by that string
	cwdSplit := strings.Split(string(out), "fcwd\nn")
	if len(cwdSplit) < 1 {
		// No cwd, return empty and nil
		return "", nil
	}

	// If there is one, we have a cwd, split the 2nd element in the array by newlines, then select the first bit of that
	// to get the cwd as a string
	return strings.Split(cwdSplit[1],"\n")[0], nil

}

// parseLsof() takes the raw string output of lsof and converts it to a slice of process structs based on the parsing
// criteria given to it in a settings struct
func parseLsof(raw string, options settings) ([]process, error) {
	// Input will be a string. Processes are separated by \np (newline, then 'p' character)
	separated := strings.Split(raw, "\np")

	// Create a new slice of processes
	allProcesses := make([]process,0)

	// For each process
	for processIndex, processString := range separated {
		// Start by splitting process info by \nf (newline, then 'f' character) to get each port open as a separate item
		// in a slice, as well as the PID, command, and user in the 1st item in the array
		connectionSplit := strings.Split(processString, "\nf")

		// there will always be a 1st (index 0) element, which should contain the following lines:
		// 88917 (process ID - no initial character as we split using that character earlier EXCEPT in process
		// with index 0, as no newline before it)
		// cProcessName
		// LUserName

		// So get that element, and split by newlines. Check if index is 0, and if so, handle first line to include the
		// 'p' character

		processInfo := strings.Split(connectionSplit[0], "\n")
		if processIndex == 0 {
			// remove first character from the first line in processInfo if we're on index 0
			processInfo[0] = processInfo[0][1:]
		}
		pid, err := strconv.Atoi(processInfo[0])
		if err != nil {
			return nil, err
		}

		finalCwd := ""
		// We have the pid, so we can use that to get the CWD.
		if options.getCwd {
			cwd, err := getCwd(pid)
			if err != nil {
				return nil, err
			}
			finalCwd = cwd
		}


		cmd := processInfo[1][1:]

		// If we don't have a process name filter OR if the current name contains one of the valid names
		if (!(len(options.nameFilter) > 0)) || (slices.Contains(options.nameFilter,cmd)) {

			user := processInfo[2][1:]

			// Now onto handling ports and addresses. Looping through each one to parse it.
			allConnections := make([]connection, 0)

			// Ignore first element in array, as we've already parsed it
			for _, connectionString := range connectionSplit[1:] {
				valid := true // Store if the connection is valid based on the parsing settings

				tmpConnection := connection{}

				connectionInfo := strings.Split(connectionString, "\n")
				// A port string will consist of a file descriptor (unused), a connection type (TCP or UDP), the
				// information on the connection (localAddress:localPort->remoteAddress:remotePort), the connection status
				// (established, listening, closed, or an empty field), and size of the read/send buffers (unused).

				// Loop through each property in the connection info


				for _, connectionProperty := range connectionInfo {
					if len(connectionProperty) > 0 {

						switch string(connectionProperty[0]){
						// Switch-case for each identifier (with an additional nested switch-case for the "T**= options)
						case "P":
							// P: Protocol
							tmpConnection.protocol = connectionProperty[1:]
							break

						case "n":
							// n: Local and remote addresses and ports

							if connectionProperty[1:] == "*:*" {
								// *:* usually indicates some unimportant connection, so we just make that connection invalid
								// This might be wrong! If you want to submit an issue about this, then feel free!
								valid = false

							} else {

								splitLocalAndRemote := strings.Split(connectionProperty[1:], "->")
								// If there is a ->, then there is a clear local and remote connection
								if len(splitLocalAndRemote) > 1 {
									// Should only be 2 elements in that array: local and remote addr:port pairs
									splitLocalAddressAndPort := strings.Split(splitLocalAndRemote[0], ":")
									splitRemoteAddressAndPort := strings.Split(splitLocalAndRemote[1], ":")

									// Set the struct's data to the parsed output
									tmpConnection.localAddress = splitLocalAddressAndPort[0]
									tmpConnection.localPort = splitLocalAddressAndPort[1]
									tmpConnection.remoteAddress = splitRemoteAddressAndPort[0]
									tmpConnection.remotePort = splitRemoteAddressAndPort[1]


								} else {
									// if not, then assume we're looking at a local port and address
									// Note here, that might be an incorrect assumption, please correct me if I'm wrong :)

									splitLocalAddressAndPort := strings.Split(splitLocalAndRemote[0], ":")
									tmpConnection.localAddress = splitLocalAddressAndPort[0]

									// As localPort is a string, we can handle ports like '*' without conversions.
									tmpConnection.localPort = splitLocalAddressAndPort[1]

								}

								// Check validity with ports

								// If we have ports to filter by
								if len(options.portFilter) > 0 {

									// If neither remote nor local ports are in the filter then it's invalid
									if !(slices.Contains(options.portFilter,tmpConnection.localPort) ||
										 slices.Contains(options.portFilter,tmpConnection.remotePort)) {
										valid = false
									}
								}


							}

							break

						case "T":
							if connectionProperty[0:4]  == "TST=" {
								// TST= : Connection status
								tmpConnection.status = connectionProperty[4:]


								// If the port isn't closed OR we have enabled closed ports
								if connectionProperty[4:] == "CLOSED" && options.showClosed {
									valid = false
								}
								if options.listenOnly && connectionProperty[4:] != "LISTEN" {
									valid = false
								}
							}
							break

						}
					}



				}

				// That connection has been parsed! Time to add it to the slice.
				if valid {
					allConnections = append(allConnections, tmpConnection) // Add the connection to the slice
				}
			}

			// All elements of a process have now been parsed, so create a new process struct with that information and
			// append it to the allProcesses slice.

			// If the process still has a valid connection in it.
			if len(allConnections) > 0 {
				// Then add it to the slice
				allProcesses = append(allProcesses, process{
					id: pid,
					name: cmd,
					username: user,
					directory: finalCwd,
					connections: allConnections,
				})
			}

		}
	}

	// Gone through all processes, so now return the final slice of process structs
	return allProcesses, nil
}

// formatLsof() takes the slice of process structs given and converts to the table rows that get rendered
func formatLsof(processes []process, options settings) ([]table.Row, []int, error) {
	// Loop through each process, and create a row based on the columns we have, then add that to a row slice
	var rows []table.Row
	var rowStarts []int

	for _, proc := range processes {
		rowStarts = append(rowStarts, len(rows))

		for connIndex, conn := range proc.connections {

			row := make(table.Row, len(options.columns))

			// Loop through each column in options.columns and use a switch-case on its title to get the value to set at its index
			for columnIndex, column := range options.columns {

				value := ""

				switch column.Title {

				case "PID":
					if connIndex == 0 {
						value = strconv.Itoa(proc.id)
					}
					break

				case "Name":
					if connIndex == 0 {
						value = proc.name
					}
					break

				case "Directory":
					if connIndex == 0 {
						value = proc.directory
					}
					break

				case "Owner":
					if connIndex == 0 {
						value = proc.username
					}
					break

				case "Protocol":
					value = conn.protocol
					break

				case "Address":
					// If there is a remote address, use that
					if conn.remoteAddress != "" {
						value = conn.remoteAddress
					} else {
						// If not, then use local address
						value = conn.localAddress
					}
					break

				case "Port":
					if conn.remotePort != "" {
						value = conn.remotePort
					} else {
						value = conn.localPort
					}
					break

				case "Local Address":
					value = conn.localAddress
					break
				case "Local Port":
					value = conn.localPort
					break
				case "Remote Address":
					value = conn.remoteAddress
					break
				case "Remote Port":
					value = conn.remotePort
					break

				case "Status":
					value = strings.ToTitle(conn.status)
					break


				}
				row[columnIndex] = value

			}
			rows = append(rows, row)

		}




	}

	return rows, rowStarts, nil

}

// ---------------------------------------------------------------------------------------------------------------------

// Func to create a command that will terminate a given process ID
func terminateProcess(id int) tea.Cmd {
	return func() tea.Msg {
		pid := id
		cmd := exec.Command("kill", strconv.Itoa(pid))

		// Terminate the process with that ID. Don't care about the output, so just ignore it
		err := cmd.Run()

		if err != nil {
			return errMsg{err}
		}
		return terminateMsg{}
	}
}

// ---------------------------------------------------------------------------------------------------------------------

// All the stuff relating to the bubbletea TUI. This includes the Init, Update, and View functions.

func (m model) Init() tea.Cmd {
	// When we first run, we want to get all the processes currently running
	return checkProcesses(m.settings)

}

// ---------------------------------------------------------------------------------------------------------------------

// Update function. Handles msgs and returns cmds for tea to run

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case processesMsg:
		// We have processes, lets update the model to use the new processes
		m.table.SetRows(msg.rows) // Convert the array of process structs to text for use in rendering
		m.rowStarts = msg.ends // The starts of each process's set of rows
		m.processes = msg.processes
		return m, nil

	case terminateMsg:
		// terminate process worked, so rerender processes table
		return m, checkProcesses(m.settings)

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.WindowSizeMsg:
		m.help.Width = msg.Width


	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Refresh):
			return m, checkProcesses(m.settings)

		case key.Matches(msg, keys.Terminate):
			// If there are any processes left:
			if len(m.processes) > 0 {
				// Get the id of the currently highlighted process and terminate that process
				cursor := m.table.Cursor()

				// Use the start of each process' set of rows to get the PID to kill.
				i := 0
				for i < (len(m.processes)){
					if i >= cursor {
						// We now have the index of the process in m.processes that we need the PID from stored in i
						return m, terminateProcess(m.processes[i].id)
					}

					i += 1
				}
				// If it breaks, do nothing
				return m, nil
			}
			return m, nil

		case key.Matches(msg, keys.Help):
			m.help.ShowAll = !m.help.ShowAll

		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		}
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}




func (m model) View() string {

	var final string
	final += baseStyle.Render(m.table.View()) + "\n"

	if m.err != nil {
		final += m.err.Error() + "\n"
	}

	helpView := m.help.View(m.keys)
	height := 17 - strings.Count(final, "\n") - strings.Count(helpView, "\n")



	return "\n" + final + strings.Repeat("\n", height) + helpView

}

func main() {
	// Start by handling the CLI switches/flags
	// Columns to enable (always enable the port column)
	flagConnStatus := pflag.BoolP("show-status","s", true, "Show the status of connections")
	flagProtocol := pflag.BoolP("show-protocol", "P", false, "Show the protocol used in a connection")
	flagShowAddresses := pflag.BoolP("show-addresses", "a",false, "Show IP addresses in a connection")
	flagFullConnection := pflag.BoolP("show-full-connection", "C", false, "Show full connection information")
	flagOwner := pflag.BoolP("show-owner","o",false, "Show the owner of processes")
	flagName := pflag.BoolP("show-process-name", "n", false, "Show the name of processes")
	flagPID := pflag.BoolP("show-process-id", "i", true, "Show the process ID")
	flagDirectory := pflag.BoolP("show-cwd", "d", false, "Show the process' current working directory")


	// Process filtering options (used in parseLsof())
	flagListeningOnly := pflag.BoolP("listen-only","l",false,"Only show listening ports")
	flagShowClosed := pflag.BoolP("show-closed", "c", false, "Show closed ports")

	// Read-only mode (prevents process termination, passed to model)
	flagReadOnly := pflag.BoolP("read-only",  "r", false, "Read-only mode - prevents processes from being terminated in the TUI")

	// A flag to set a comma separated list of ports to filter by
	flagPortFilter := pflag.StringSlice("ports",nil,"Port filter - only shows the selected ports. Accepts a list of port numbers, separated by commas.")

	// Help command should be built-in, and populates based in usage field in pflag.TypeP()
	pflag.Parse()

	// All other args act as a process name filter
	cmdArgs := pflag.Args()

	// Create a settings map with columns and bool values. Note that pflag makes the variables pointers,
	// hence the need for *variable

	columnSettings := map[table.Column]bool{
		// Process information
		table.Column{Title: "PID", Width: 5}:      			*flagPID,
		table.Column{Title: "Name", Width: 10}:    			*flagName,
		table.Column{Title: "Directory", Width: 16}:    	*flagDirectory,
		table.Column{Title: "Owner", Width: 8}:    			*flagOwner,

		// Connection information
		table.Column{Title: "Protocol", Width: 3}: 			*flagProtocol,
		// Used when not viewing full connection
		table.Column{Title: "Address", Width: 15}: 			*flagShowAddresses && !*flagFullConnection,
		table.Column{Title: "Port", Width: 5}: 				!*flagFullConnection,
		// Used when viewing full connection
		table.Column{Title: "Local Address", Width: 15}: 	*flagFullConnection,
		table.Column{Title: "Local Port", Width: 5}: 		*flagFullConnection,
		table.Column{Title: "Remote Address", Width: 15}: 	*flagFullConnection,
		table.Column{Title: "Remote Port", Width: 5}: 		*flagFullConnection,

		table.Column{Title: "Status", Width: 11}: 			*flagConnStatus,

	}

	columnIndexes := []table.Column{
		{Title: "PID", Width: 5},
		{Title: "Name", Width: 10},
		{Title: "Directory", Width: 16},
		{Title: "Owner", Width: 8},

		// Connection information
		{Title: "Protocol", Width: 3},
		// Used when not viewing full connection
		{Title: "Address", Width: 15},
		{Title: "Port", Width: 5},
		// Used when viewing full connection
		{Title: "Local Address", Width: 15},
		{Title: "Local Port", Width: 5},
		{Title: "Remote Address", Width: 15},
		{Title: "Remote Port", Width: 5},

		{Title: "Status", Width: 11},

	}


	// Configure columns to use by looping through columnSettings
	var columns []table.Column

	for i := 0; i < len(columnSettings); i++ {
		colName := columnIndexes[i]
		enableColumn := columnSettings[colName]

		if enableColumn {
			columns = append(columns, colName) // If the settings allow it, enable the column.
		}
	}

	// Set to empty, then let commands etc. fill the rows out
	rows := []table.Row{}

	// Create a new table with the selected columns
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	// Change the default styles of the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)


	// Create settings struct for parsing settings and render columns
	parseAndRenderSettings := settings{
		readOnly:   *flagReadOnly,
		showClosed: *flagShowClosed,
		listenOnly: *flagListeningOnly,
		getCwd: 	*flagDirectory,
		columns:    columns,
		nameFilter: cmdArgs,
		portFilter: *flagPortFilter,
	}

	// Create final model struct
	m := model{
		table:      t,
		processes:  []process{},
		err:        nil,
		settings:   parseAndRenderSettings,

		keys:       keys,
		help:       help.New(),
		inputStyle: baseStyle,
	}

	// Run it! (except if we're running on Windows)
	if runtime.GOOS == "windows" {
		fmt.Println("Sorry, pvw is UNIX only right now.")
	} else {
		// Check if lsof is installed
		cmd := exec.Command("/bin/sh", "-c", "command -v lsof")
		err := cmd.Run()

		if err != nil {
			fmt.Println("Error running pvw: lsof command does not exist. Please install lsof with your package manager.")
			os.Exit(1)

		}


		if _, err := tea.NewProgram(m).Run(); err != nil {
			fmt.Println("Error running pvw: ", err)
			os.Exit(1)
		}
	}

}