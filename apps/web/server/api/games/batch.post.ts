import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { batchGames } from "../../bff/handlers";

export default defineEventHandler((event) => batchGames(event, useRuntimeConfig(event)));
