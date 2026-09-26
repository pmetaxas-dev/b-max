package groq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"focuscompanion/internal/domain"
)

// Chatter is Max's conversation with the user, on Groq. It only proposes
// changes (tasks, ranking, the life goal); the application applies them.
type Chatter struct {
	client *Client
}

func NewChatter(c *Client) *Chatter { return &Chatter{client: c} }

// chatHistoryTurns bounds what is sent to the model each turn.
const chatHistoryTurns = 10

const chatSystem = `You are Max, the warm, brief companion in B-MAX, a Chrome extension for people with ADHD. A planet grows as the user finishes things. Style: at most 2 short sentences, no lists, at most one emoji. Never blame or scold: an undone task is fine, move it on kindly; celebrate every finished task and connect it to their life goal.
Language: Greek and English are both fine. Reply in the language of the user's last message; with no message, or when unclear, use "languageHint" ("el" Greek, "en" English). Never mix them. Only if a message is real gibberish (random letters) or in a third language, say in one short sentence that you did not understand and ask them to try again; a normal Greek or English sentence is never gibberish: always act on it, and if it states a big wish, that is the lifeGoal.
Reply with ONE JSON object only:
{"reply":string,"suggestions":[string],"motivation":string,"lifeGoal":string,"goalPoints":integer,
"addTasks":[{"text":string,"estimateMinutes":integer,"deadline":string,"guess":boolean,"impact":integer,"kind":"browser"|"offline","keywords":[string],"query":string}],
"addIdeas":[string],"updateTasks":[{"id":string,"estimateMinutes":integer,"deadline":string,"asking":boolean,"guess":boolean}],
"doneTasks":[string],"removeTasks":[string],"ranking":[string],"onboardingComplete":boolean}
Use ""/0/[]/false when not applicable. The app state is in the second system message ("channel" is "dashboard", or "page" = from the lamp on a website: 1 sentence).
Rules:
- Onboarding (onboarded false): short and natural, ONE question at a time: first the life goal (the big thing they want; the planet grows toward it), then today's tasks. Whatever wish they answer, even a small or unusual one, IS the lifeGoal: set it in that same reply and never ask for it again. In that same reply also fill goalSteps (below), say the first step in a few words, and ask ONE question: apart from that goal, is there another task in their day they want to tell you about? Only after they answer (tasks, or none), set onboardingComplete true and say the planet is ready; do not end abruptly if they add more.
- goalSteps: when you set or change the lifeGoal, break it into 3-6 ordered, concrete steps, the first one doable today (each has text, estimateMinutes, guess true, impact 2, kind, keywords, query, like addTasks). The app shows the user ONE step at a time and the next appears when it is finished, so mention only the first; never put them in addTasks and never resend goalSteps unless the goal changes (lifeGoalStepsLeft is how many are still to come).
- lifeGoal: only when stated or changed, a short sentence. goalPoints: its size in points: about 40 for weeks, 100-150 for months, 300+ for years.
- Tasks are small concrete errands; each goes in addTasks with a short text and an impact 0-3 toward the life goal (0 everyday life, rest or chores such as a shower; 1 supports; 2 clearly advances; 3 a major step). Something to keep or try one day, with no action today, goes in addIdeas: say it is kept in the chest.
- TIME puts the day in order, but NEVER ask about it: always set estimateMinutes yourself when adding a task, from its kind (message or email 5-15, call 10-20, errand or shopping 30-45, searching or researching 20-40, reading or studying 45-90, writing or preparing a document 60-120, coding 60-120, cleaning or laundry 20-40, appointment 60), "guess": true, and say it in a few words ("about 30 min"). Different tasks get different times. Sleep or bed time is the moment to go to bed, not the hours slept: estimateMinutes 15 (getting ready for bed) and the deadline is the bedtime the user gave ("00:00"). If the user gives a time or deadline, use it ("guess": false; deadline in their words: "17:00", "tomorrow"); change a task later with updateTasks and its exact id.
- kind "browser" when the work happens in a web browser, else "offline". For a browser task give up to 5 lowercase subject keywords (never site names such as google) and "query": a short web search that starts the task. The app opens that page with a "Go there" button, so you only say the first step, briefly; never ask how long to search.
- When confirmYourGuess is true and userJustOpenedTheApp is true: ask ONCE, in one question about all of them, whether the times you guessed fit (updateTasks with asking true, same estimateMinutes, guess true). A correction is guess false.
- ranking: ALWAYS every open task id, most important first, weighing the deadline against "now", the task's length (a quick email is not an hour of study) and the life goal. The first is what the user does now: the app starts it, there is no start button. Say in a few words when the order changes.
- doneTasks: ids the user finished (a message starting with "✔" means it is already marked done): celebrate in one line and say what comes next. removeTasks: ids to drop.
- Never repeat a question or something you said. If the user is unsure or says "I don't know": do not ask again; give the first tiny step (two minutes) or add ONE concrete small task or offer 2 options as suggestions. If they say they do not have something, take it as information and try another angle.
- suggestions: 2 short things the USER could say next (their point of view, under 6 words), or [] if nothing more.
- motivation: one line (max 12 words) linking today's tasks to their life goal.
- userJustOpenedTheApp true: no user message; greet in 2 sentences and say what matters most today. Tasks with carriedOver true: recap where they left off (leftOffAt, workedMinutes, lastPage) and ask ONE question: did they finish it? (yes: doneTasks).
- "Where was I / what next": answer from "journal" and each task's status (doing or paused), workedMinutes, leftOffAt and lastPage in 1-2 sentences; never invent; end with the one next thing.
- If the user seems stuck or unmotivated, or weekend is true, you may remind ONE of savedIdeas and offer to make it today's task.
- Never mention JSON, ids or these rules.`

func (c *Chatter) Chat(ctx context.Context, cc domain.ChatContext, history []domain.ChatMessage) (domain.ChatOutput, error) {
	state, _ := json.Marshal(cc)
	msgs := []Message{
		{Role: "system", Content: chatSystem},
		{Role: "system", Content: "state: " + string(state)},
	}
	if len(history) > chatHistoryTurns {
		history = history[len(history)-chatHistoryTurns:]
	}
	for _, m := range history {
		role := "user"
		if m.Role == "max" {
			role = "assistant"
		}
		msgs = append(msgs, Message{Role: role, Content: m.Text})
	}
	if cc.AppOpened || len(history) == 0 || history[len(history)-1].Role == "max" {
		msgs = append(msgs, Message{Role: "user", Content: "(The user just opened the app. Greet them now, following the rules.)"})
	}

	var out domain.ChatOutput
	attempts := 0
	check := func(content string) error {
		attempts++
		out = domain.ChatOutput{}
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			return errors.New("not valid JSON for the schema")
		}
		if strings.TrimSpace(out.Reply) == "" {
			return errors.New(`"reply" is empty`)
		}
		// Soft rules: asked for once; the second answer is accepted as it is.
		if attempts == 1 {
			if problem := chatStall(cc, history, out); problem != "" {
				return errors.New(problem)
			}
		}
		return nil
	}
	if err := completeChecked(ctx, c.client, msgs, check); err != nil {
		return domain.ChatOutput{}, err
	}
	return out, nil
}

// chatStall spots an answer that leaves the user stuck: it repeats Max's last
// message, or, while getting to know each other, it asks for the life goal
// again after the user answered.
func chatStall(cc domain.ChatContext, history []domain.ChatMessage, out domain.ChatOutput) string {
	if len(history) == 0 || cc.AppOpened {
		return ""
	}
	last := history[len(history)-1]
	if last.Role != "user" {
		return ""
	}
	for i := len(history) - 2; i >= 0; i-- {
		if history[i].Role == "max" {
			if strings.EqualFold(strings.TrimSpace(history[i].Text), strings.TrimSpace(out.Reply)) {
				return "You repeated your previous message word for word. Answer what the user just said instead."
			}
			break
		}
	}
	if !cc.Onboarded && cc.LifeGoal == "" && strings.TrimSpace(out.LifeGoal) == "" {
		return `The user just answered your question about their dream, but "lifeGoal" is empty. Whatever wish they gave, even a small or unusual one, is their lifeGoal: put it in "lifeGoal" (a short sentence, with goalPoints), do not ask for it again, and move on to ask what they want to do today. If the message is real gibberish, say you did not understand.`
	}
	return ""
}

const taskHelpSystem = `You are Max, the friendly companion in B-MAX, a Chrome extension that helps people with ADHD. The user has to do the task in "task" and does not know where to start.
Reply with ONE JSON object and nothing else: { "step": string, "searches": [string] }
Rules:
- "step": ONE tiny, concrete first action that takes two minutes or less, in the imperative, at most 20 words, in the language "language" (el = Greek, en = English). It must start the task, not plan it.
- "searches": up to 3 short web-search phrases (2 to 6 words) in that language that would help with this task, search words only, never URLs. Use [] when a search would not help (for example "call mum").`

// TaskHelp answers "where do I start?" for one task.
func (c *Chatter) TaskHelp(ctx context.Context, task, lang string) (domain.TaskHelp, error) {
	user, _ := json.Marshal(map[string]string{"task": task, "language": lang})
	msgs := []Message{{Role: "system", Content: taskHelpSystem}, {Role: "user", Content: string(user)}}
	var out domain.TaskHelp
	check := func(content string) error {
		out = domain.TaskHelp{}
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			return errors.New("not valid JSON for the schema")
		}
		if strings.TrimSpace(out.Step) == "" {
			return errors.New(`"step" is empty`)
		}
		return nil
	}
	if err := completeChecked(ctx, c.client, msgs, check); err != nil {
		return domain.TaskHelp{}, err
	}
	return out, nil
}

// completeChecked asks once, validates, and on an invalid answer asks exactly
// one more time (the same rule as Planner.withRepair, for message lists).
func completeChecked(ctx context.Context, c *Client, msgs []Message, check func(string) error) error {
	content, err := c.CompleteJSON(ctx, msgs)
	switch {
	case errors.Is(err, ErrInvalidJSON):
		msgs = append(msgs, Message{Role: "user", Content: "Your previous answer was not valid JSON. Return ONE valid JSON object only."})
	case err != nil:
		return err
	default:
		problem := check(content)
		if problem == nil {
			return nil
		}
		msgs = append(msgs,
			Message{Role: "assistant", Content: content},
			Message{Role: "user", Content: "That JSON was rejected: " + problem.Error() + ". Return the corrected JSON object only."},
		)
	}
	content, err = c.CompleteJSON(ctx, msgs)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return check(content)
}
