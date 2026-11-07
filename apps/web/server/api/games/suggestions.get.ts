import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { suggestGames } from "../../bff/handlers";

export default defineEventHandler((event) => suggestGames(event, useRuntimeConfig(event)));
