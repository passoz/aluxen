import { redirect } from '@remix-run/node';
import type { LoaderFunctionArgs } from '@remix-run/node';
import { getUserId } from '../utils/session.server';

export async function loader({ request }: LoaderFunctionArgs) {
  const userId = await getUserId(request);
  throw redirect(userId ? '/dashboard?tab=tenants' : '/login');
}
