import { nanoid } from 'nanoid';
import { nanoid as asyncNanoid } from 'nanoid/async';
import { customAlphabet } from 'nanoid/non-secure';
import { urlAlphabet } from 'nanoid/url-alphabet';

export const ids = [nanoid(), customAlphabet(urlAlphabet, 8)()];
export const later = asyncNanoid;
