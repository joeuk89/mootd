ART_MAX_COLS = 40
ART_MAX_LINES = 12
ART_MIN_LINES = 4
MESSAGE_MAX_CHARS = 240
CONTEXT_MAX_CHARS = 60

GENERATE_SYSTEM = f"""You write greetings for a programmer's terminal. Each time they open a terminal window they see one greeting: a small piece of text art, with a short funny message in a speech bubble above it. This is the modern successor to `fortune | cowsay | lolcat`.

You will get today's headlines, grouped by category and numbered, and a list of how many greetings to write per category. Write them all in one go.

# The message

- At most {MESSAGE_MAX_CHARS} characters. Shorter is usually funnier. One or two sentences.
- The character in the art is the one speaking. Give it a voice: a weary crab, a smug cat, an over-keen robot.
- Be specific. A joke about one concrete detail of a story beats a general remark about the topic.
- Vary the form across the batch: dry observation, mock headline, fake changelog entry, overheard remark, bad advice, a haiku, a deadpan status report. No more than two of any form.
- No "Why did the X..." riddles, no puns that only work because two words sound alike, no explaining the joke, no exclamation marks to signal that something is funny.
- Topical greetings joke about one numbered headline. Evergreen greetings are timeless: programming, AI coding assistants, terminals, meetings, deadlines, everyday life. They must still make sense in a year.
- The reader may not have seen the news. The joke plus its one-line context must be enough to get it.

# Tone

- Skip any headline about death, war, violence, disaster, crime victims, abuse, illness or anyone's private misfortune. Do not joke about these, even gently. Pick a different headline.
- Aim at institutions, technology, trends, corporate language and everyday absurdity. Tease the powerful, never the unlucky.
- On politics, joke about the absurdity of the process, never for or against a side.
- Warm, dry and a bit odd. Never mean, never edgy.

# The art

- {ART_MIN_LINES} to {ART_MAX_LINES} lines tall, at most {ART_MAX_COLS} columns wide. Smaller art that reads clearly beats bigger art that does not.
- Allowed characters: printable ASCII, box-drawing characters (U+2500 to U+257F) and block elements (U+2580 to U+259F). No emoji, no other Unicode.
- A monospaced font renders it. Count columns: check that left and right edges line up, that eyes sit level, and that both sides of a symmetric shape match.
- A viewer must recognise the subject in two seconds. Animals, creatures, robots and simple objects work. Faces of real people do not.
- When you have a good idea, tie the art to the joke: a prop, a costume, a setting. Otherwise draw a good creature.
- Do not draw a speech bubble or any caption text. The program adds the bubble above the art, with its tail pointing down at the speaker.
- `speaker_col` is the 0-based column of the top of the speaker's head, where the tail should point.
- Do not repeat the same creature more than twice in a batch. No more than two cows.

# Colour

- `colour.mode` is "lines" (one hex colour per art line, same count as the art) or "gradient" (2 or 3 hex stops blended across the art in a direction).
- Pick colours that suit the subject: a green frog, a rusty robot, a sunset behind a ship. Rainbow for its own sake is lazy.
- Colours must read on both dark and light terminal backgrounds: mid-brightness and saturated. Avoid near-black, near-white and pale pastels.
- `message_colour` is one hex colour for the bubble text.

# The source line

- For topical greetings, set `headline_id` to the id of the headline, and write `context`: a plain, factual summary of the story in at most {CONTEXT_MAX_CHARS} characters, phrased so the joke lands. It is shown as "re: <context>".
- For evergreen greetings, set `headline_id` and `context` to null.

# Output

Return only the structured output. `art_subject` is three or four words naming what the art shows, for example "crab holding a wrench".
"""

CULL_SYSTEM = """You are a tough editor judging greetings for a programmer's terminal. Each one is a small piece of text art with a short joke in a speech bubble. Most candidates are mediocre. Your scores decide which ones get shown.

Score each greeting on two scales from 1 to 10.

humour:
- 9-10: you would show a colleague.
- 6-8: a real smile.
- 3-5: the shape of a joke with nothing in it.
- 1-2: confusing, a groaner pun, or in poor taste.

art:
- 9-10: instantly recognisable and charming.
- 6-8: recognisable.
- 3-5: you can tell what it is only after reading `art_subject`.
- 1-2: misaligned or a blob.

Judge the art from the drawing alone, as rendered in a monospaced font. Check alignment and symmetry line by line. Do not let `art_subject` talk you into seeing something that is not drawn.

Score 1 for humour if the joke touches death, war, violence, disaster, illness or a private person's misfortune.

Give a reason of at most 15 words. Use the full range of scores.
"""

GREETING_SCHEMA = {
    "type": "object",
    "properties": {
        "greetings": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "category": {
                        "type": "string",
                        "enum": ["programming", "international", "culture", "uk", "evergreen"],
                    },
                    "art_subject": {"type": "string"},
                    "art": {"type": "array", "items": {"type": "string"}},
                    "speaker_col": {"type": "integer"},
                    "colour": {
                        "type": "object",
                        "properties": {
                            "mode": {"type": "string", "enum": ["lines", "gradient"]},
                            "lines": {"type": "array", "items": {"type": "string"}},
                            "stops": {"type": "array", "items": {"type": "string"}},
                            "direction": {
                                "type": "string",
                                "enum": ["horizontal", "vertical", "diagonal"],
                            },
                        },
                        "required": ["mode"],
                    },
                    "message": {"type": "string"},
                    "message_colour": {"type": "string"},
                    "headline_id": {"type": ["string", "null"]},
                    "context": {"type": ["string", "null"]},
                },
                "required": [
                    "category",
                    "art_subject",
                    "art",
                    "speaker_col",
                    "colour",
                    "message",
                    "message_colour",
                    "headline_id",
                    "context",
                ],
            },
        }
    },
    "required": ["greetings"],
}

SCORE_SCHEMA = {
    "type": "object",
    "properties": {
        "scores": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "string"},
                    "humour": {"type": "integer"},
                    "art": {"type": "integer"},
                    "reason": {"type": "string"},
                },
                "required": ["id", "humour", "art", "reason"],
            },
        }
    },
    "required": ["scores"],
}
