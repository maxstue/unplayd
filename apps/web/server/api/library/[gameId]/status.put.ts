import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { setLibraryStatus } from "../../../bff/handlers";

export default defineEventHandler((event) => setLibraryStatus(event, useRuntimeConfig(event)));
