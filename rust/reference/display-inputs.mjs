const cases = [];
const encode = (value) => Buffer.from(value).toString("base64");
const homes = ["/home/alex", "/home/alex///", "/home/../home/alex/.", "/", "relative/../home", "", "/a\u00a0b", "/a\0b", Buffer.from([47, 255])];
const values = ["", "/home/alex", "/home/alex/file", "/home/alex-other", "/home/alex/../x", "open /home/alex/file: missing", "'/home/alex' and /home/alex/file", "x/home/alex/file", "α/home/alex/file", "\u00a0/home/alex/file", "/a\u00a0b/file", "/x", "/home", "home/file", Buffer.from([47, 255, 47, 254])];
for (const fn of ["PathBytes", "ErrorBytes"]) {
  for (const home of homes) for (const value of values) for (const present of [true, false]) cases.push({ fn, args: [encode(value), encode(home), present] });
  for (let byte = 0; byte < 256; byte++) {
    cases.push({ fn, args: [encode(Buffer.concat([Buffer.from([byte]), Buffer.from("/home/alex"), Buffer.from([byte])])), encode("/home/alex"), true] });
  }
  for (const args of [["!", "", true], ["", "!", true], ["AA==\n", "", false]]) cases.push({ fn, args });
}
for (const fn of ["FormatPath", "FormatErrorText"]) for (const value of values.filter((value) => typeof value === "string")) cases.push({ fn, args: [value] });
await Bun.write(new URL("display-inputs.jsonl", import.meta.url), cases.map((value) => JSON.stringify(value)).join("\n") + "\n");
