import { common } from "./common";
import { auth } from "./auth";
import { memory } from "./memory";
import { stats } from "./stats";
import { insights } from "./insights";
import { add } from "./add";
import { jobs } from "./jobs";
import { embedding } from "./embedding";
import { preview } from "./preview";
import { chat } from "./chat";

export const es = {
  ...common,
  ...auth,
  ...memory,
  ...stats,
  ...insights,
  ...add,
  ...jobs,
  ...embedding,
  ...preview,
  ...chat,
};
