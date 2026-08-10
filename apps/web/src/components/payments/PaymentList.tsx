import { Fragment } from 'react'
import { Separator } from '@/components/ui/separator'
import PaymentGroup, { type PaymentGroupData } from './PaymentGroup'
import type { Entry } from '@/api/types'

interface Props {
  groups: PaymentGroupData[]
  onMarkPaid?: (entry: Entry, method: string) => void
  onDelete?: (entry: Entry) => void
}

// Renders a list of grouped, urgency- or period-bucketed entries. Callers
// own the surrounding Card/CardContent chrome (this just fills it).
export default function PaymentList({ groups, onMarkPaid, onDelete }: Props) {
  return (
    <div className="py-2">
      {groups.map((group, i) => (
        <Fragment key={group.key}>
          {i > 0 && <Separator className="my-4" />}
          <PaymentGroup group={group} onMarkPaid={onMarkPaid} onDelete={onDelete} />
        </Fragment>
      ))}
    </div>
  )
}
