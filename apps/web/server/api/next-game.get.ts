import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { drawNextGame } from "../bff/handlers";

export default defineEventHandler((event) => drawNextGame(event, useRuntimeConfig(event)));
