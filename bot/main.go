package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v31/github"
	"github.com/posener/goaction"
	"github.com/posener/goaction/actionutil"
	"github.com/posener/goaction/log"
)

//goaction:required
//goaction:description A token for Github APIs.
var token = os.Getenv("GITHUB_TOKEN")

func main() {
	ctx := context.Background()

	if !goaction.CI {
		log.Debugf("Not in Github action mode, quiting.")
		return
	}

	if goaction.Event != goaction.EventIssues {
		log.Debugf("Not an issue action, nothing to do here.")
		return
	}

	issue, err := goaction.GetIssues()
	if err != nil {
		log.Errorf("Failed getting issue information: %s", err)
		os.Exit(1)
	}

	// Create a Github Client using the token provided through environment.
	if token == "" {
		log.Errorf("Token was not provided, please define the Github action 'with' 'github-token' as '${{ secrets.GITHUB_TOKEN }}'")
	}
	gh := actionutil.NewClientWithToken(ctx, token)

	fail := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		log.Errorf(msg)
		gh.IssuesCreateComment(ctx, issue.GetIssue().GetNumber(), &github.IssueComment{
			Body: github.String(msg),
		})
		os.Exit(1)
	}

	// Interact with the create issue according to the triggering action:
	if action := issue.GetAction(); action != "labeled" {
		log.Debugf("Ignoring issue action: %q", action)
		return
	}
	if label := issue.GetLabel().GetName(); label != "approved" {
		log.Debugf("Ignoring label %s", label)
		return
	}

	newQ, err := parseBody(issue.Issue.GetBody())
	if err != nil {
		fail("Failed pare question body: %s", err)
	}

	path := newQ.page + ".json"

	file, err := os.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		fail("Failed open %q: %s", path, err)
	}
	defer file.Close()

	var questions []*Question

	err = json.NewDecoder(file).Decode(&questions)
	if err != nil {
		fail("Failed decode %q: %s", path, err)
	}

	questions = append(questions, newQ)

	// Write the new questions.
	err = file.Truncate(0)
	if err != nil {
		fail("Failed file truncate: %s", err)
	}

	e := json.NewEncoder(file)
	e.SetIndent("", "  ")
	err = e.Encode(questions)
	if err != nil {
		fail("Failed encoding questions: %s", err)
	}

	err = actionutil.GitConfig("bot", "bot@learn.github.com")
	if err != nil {
		fail("Failed git config: %s", err)
	}
	err = actionutil.GitCommitPush([]string{path}, fmt.Sprintf("Update question from issue #%d", *issue.Issue.ID))
	if err != nil {
		fail("Failed encoding questions: %s", err)
	}

	// Close issue.
	_, _, err = gh.IssuesEdit(ctx, issue.Issue.GetNumber(), &github.IssueRequest{State: github.String("closed")})
	if err != nil {
		fail("Failed closing issue: %s", err)
	}
}

type Question struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   int      `json:"answer"`
	Explain  string   `json:"explain"`

	page string
}

type state string

const (
	stateNone         state = "none"
	stateReadQuestion state = "question"
	stateReadAnswer   state = "answer"
	stateReadOption   state = "option"
	stateReadExplain  state = "explain"
	stateReadPage     state = "page"
)

func parseBody(body string) (*Question, error) {
	var q Question

	state := stateNone
	currentValue := ""

	s := bufio.NewScanner(strings.NewReader(body))
scan:
	for s.Scan() {
		if s.Err() != nil {
			break
		}
		line := s.Text()
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue scan
		case strings.HasPrefix(line, "### "):
			// Handle last instruction
			err := q.set(state, currentValue)
			if err != nil {
				return nil, fmt.Errorf("failed set state %q to %q", state, currentValue)
			}
			currentValue = ""

			// Handle new state
			instruction := strings.TrimPrefix(line, "### ")
			switch {
			case instruction == "question":
				state = stateReadQuestion
			case strings.HasPrefix(instruction, "option"):
				state = stateReadOption
			case instruction == "answer":
				state = stateReadAnswer
			case instruction == "explain":
				state = stateReadExplain
			case instruction == "page":
				state = stateReadPage
			default:
				return nil, fmt.Errorf("unknown instruction %q", instruction)
			}
		default:
			currentValue += line
		}
	}
	err := q.set(state, currentValue) // Set the last state.
	if err != nil {
		return nil, fmt.Errorf("failed set state %q to %q", state, currentValue)
	}

	return &q, nil
}

func (q *Question) set(state state, value string) error {
	var err error
	switch state {
	case stateReadQuestion:
		q.Question = value
	case stateReadOption:
		q.Options = append(q.Options, value)
	case stateReadAnswer:
		q.Answer, err = strconv.Atoi(value)
	case stateReadExplain:
		q.Explain = value
	case stateReadPage:
		q.page = value
	}
	return err
}
