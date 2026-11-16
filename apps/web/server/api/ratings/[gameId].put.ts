import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { setRating } from "../../bff/handlers";

export default defineEventHandler((event) => setRating(event, useRuntimeConfig(event)));
