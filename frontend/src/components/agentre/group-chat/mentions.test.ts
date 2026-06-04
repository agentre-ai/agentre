import { describe, it, expect } from "vitest";

import {
  parseMentionedMemberIds,
  tokenizeMentions,
  type MentionRosterEntry,
} from "./mentions";

describe("parseMentionedMemberIds", () => {
  it("returns the id of a single matched @name", () => {
    const roster: MentionRosterEntry[] = [{ memberId: 2, name: "后端" }];
    expect(parseMentionedMemberIds("麻烦 @后端 看下", roster)).toEqual([2]);
  });

  it("does NOT match @Bob inside @Bobby (substring guard)", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 7, name: "Bob" },
      { memberId: 8, name: "Bobby" },
    ];
    // @Bobby must resolve to Bobby (8), never to the Bob (7) prefix.
    expect(parseMentionedMemberIds("hi @Bobby there", roster)).toEqual([8]);
  });

  it("matches a multi-word name like @Code Reviewer", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 4, name: "Code Reviewer" },
      { memberId: 5, name: "Code" },
    ];
    // longest-first → "Code Reviewer" wins over "Code".
    expect(parseMentionedMemberIds("ping @Code Reviewer now", roster)).toEqual([
      4,
    ]);
  });

  it("derives from text, so a deleted @name is no longer a recipient", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 7, name: "Bob" },
      { memberId: 8, name: "Bobby" },
    ];
    // user had chosen Bob earlier but deleted "@Bob", only "@Bobby" remains.
    expect(parseMentionedMemberIds("hello @Bobby", roster)).toEqual([8]);
    expect(parseMentionedMemberIds("hello @Bobby", roster)).not.toContain(7);
  });

  it("returns both ids for two distinct mentions, de-duped & order-stable", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 2, name: "后端" },
      { memberId: 3, name: "前端" },
    ];
    expect(
      parseMentionedMemberIds("@后端 和 @前端 还有 @后端 一起", roster),
    ).toEqual([2, 3]);
  });

  it("returns none for an unknown @stranger", () => {
    const roster: MentionRosterEntry[] = [{ memberId: 2, name: "后端" }];
    expect(parseMentionedMemberIds("@陌生人 hi", roster)).toEqual([]);
  });

  it("matches a mention at the start and at the very end of the string", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 2, name: "后端" },
      { memberId: 3, name: "前端" },
    ];
    expect(parseMentionedMemberIds("@后端", roster)).toEqual([2]);
    expect(parseMentionedMemberIds("收到 @前端", roster)).toEqual([3]);
  });

  it("treats a name with regex-special chars literally and never throws", () => {
    const roster: MentionRosterEntry[] = [
      { memberId: 9, name: "C++ (dev)" },
      { memberId: 1, name: "C" },
    ];
    expect(() =>
      parseMentionedMemberIds("ask @C++ (dev) please", roster),
    ).not.toThrow();
    expect(parseMentionedMemberIds("ask @C++ (dev) please", roster)).toEqual([
      9,
    ]);
  });
});

describe("tokenizeMentions", () => {
  const roster: MentionRosterEntry[] = [
    { memberId: 2, name: "后端" },
    { memberId: 3, name: "前端" },
  ];

  it("turns a matched @name into a mention segment with the right memberId", () => {
    expect(tokenizeMentions("麻烦 @后端 看下", roster)).toEqual([
      { type: "text", value: "麻烦 " },
      { type: "mention", memberId: 2, name: "后端" },
      { type: "text", value: " 看下" },
    ]);
  });

  it("leaves an unmatched @name as plain text", () => {
    expect(tokenizeMentions("@陌生人 hi", roster)).toEqual([
      { type: "text", value: "@陌生人 hi" },
    ]);
  });

  it("recognizes <mention>NAME</mention> markup", () => {
    expect(tokenizeMentions("好的 <mention>前端</mention>", roster)).toEqual([
      { type: "text", value: "好的 " },
      { type: "mention", memberId: 3, name: "前端" },
    ]);
  });

  it("preserves ordering of mixed text and multiple mentions", () => {
    expect(tokenizeMentions("@后端 然后 @前端 收工", roster)).toEqual([
      { type: "mention", memberId: 2, name: "后端" },
      { type: "text", value: " 然后 " },
      { type: "mention", memberId: 3, name: "前端" },
      { type: "text", value: " 收工" },
    ]);
  });

  it("matches multi-word names and beats shorter prefixes (longest-first)", () => {
    const r: MentionRosterEntry[] = [
      { memberId: 4, name: "Code Reviewer" },
      { memberId: 5, name: "Code" },
    ];
    expect(tokenizeMentions("ping @Code Reviewer now", r)).toEqual([
      { type: "text", value: "ping " },
      { type: "mention", memberId: 4, name: "Code Reviewer" },
      { type: "text", value: " now" },
    ]);
  });

  it("does not let @Bob chip-match inside @Bobby", () => {
    const r: MentionRosterEntry[] = [
      { memberId: 7, name: "Bob" },
      { memberId: 8, name: "Bobby" },
    ];
    expect(tokenizeMentions("hi @Bobby", r)).toEqual([
      { type: "text", value: "hi " },
      { type: "mention", memberId: 8, name: "Bobby" },
    ]);
  });

  it("handles a name with regex-special chars literally", () => {
    const r: MentionRosterEntry[] = [{ memberId: 9, name: "C++" }];
    expect(() => tokenizeMentions("use @C++ ok", r)).not.toThrow();
    expect(tokenizeMentions("use @C++ ok", r)).toEqual([
      { type: "text", value: "use " },
      { type: "mention", memberId: 9, name: "C++" },
      { type: "text", value: " ok" },
    ]);
  });
});
